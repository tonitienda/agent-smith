// Package clean computes and applies manual context edits — the engine behind
// `/clean` (AS-028, PRD §7.12, D3), the second flagship wedge's v1 half. The
// user picks segments from the composition view (AS-026) by their stable block
// handle; clean previews exactly what leaves the window and the tokens/$ it
// reclaims, then applies the edit as an appended exclusion event. History is
// never mutated: a removal is an eventlog.KindExclusion naming the dropped
// blocks, and an undo is a further exclusion targeting that exclusion, so the
// projection (AS-006) restores exactly (PRD D3).
//
// The natural-language matcher (`/clean "<topic>"`) is AS-029; this package is
// the manual-selection foundation it will build on — preview, atomic pairing,
// exclusion events, and undo all live here.
package clean

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// Producers attribute the exclusion events clean appends, so an undo can find
// the removals it should reverse without mistaking its own counter-events for
// removals (see Undo).
const (
	// ApplyProducer attributes a removal exclusion.
	ApplyProducer = "/clean"
	// UndoProducer attributes the counter-exclusion an undo appends.
	UndoProducer = "/clean --undo"
)

// recentAge flags a "very recent" block: removing something this fresh is more
// likely a mistake (the live thread may still depend on it), so the preview
// warns rather than refusing — the user stays in control (PRD §6 guardrail).
const recentAge = 2 * time.Minute

// Item is one block a plan removes, with the dimensions the preview shows.
type Item struct {
	ID      string      // stable block handle (the selection key)
	Kind    schema.Kind // block kind
	Origin  string      // file path, tool name, or role
	Tokens  int         // estimated window share reclaimed
	CostUSD float64     // Tokens priced at the active model's input rate
	Age     time.Duration
	// Paired is true when the block was pulled into the plan to keep a
	// tool-call/result pair atomic, not selected directly (AS-028 guardrail).
	Paired bool
}

// Plan is the previewed effect of a selection. Building one mutates nothing; it
// is the "preview pane" the user confirms before any change is applied (AC).
type Plan struct {
	Items    []Item   // blocks to remove, largest first
	Tokens   int      // total estimated tokens reclaimed
	CostUSD  float64  // total cost reclaimed at the input rate
	Priced   bool     // false when the active model is unpriced ($ blank)
	Currency string   // money prefix, e.g. "$"
	Warnings []string // soft guardrail notes (e.g. very recent blocks)
	Unknown  []string // selectors that matched no live segment
}

// Empty reports whether the plan would remove nothing.
func (p Plan) Empty() bool { return len(p.Items) == 0 }

// IDs returns the block IDs the plan removes, in plan order. These are the
// targets of the exclusion event Apply builds.
func (p Plan) IDs() []string {
	out := make([]string, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.ID
	}
	return out
}

// Preview resolves selectors against proj's live window and computes the plan:
// which blocks would be removed (expanded to keep tool-call/result pairs
// atomic), the tokens/$ reclaimed, and any guardrail warnings. selectors are
// block handles — a full ID or any unambiguous prefix (the "blk_" prefix is
// optional), as surfaced by the /context view. It is pure and never mutates the
// log.
func Preview(proj *projection.Projection, table *cost.Table, model string, now time.Time, selectors []string) Plan {
	// Reuse the composition so preview token/$ figures match /context exactly.
	comp := composition.Build(proj, table, model, now, composition.SortSize)
	live := make(map[string]composition.Segment, len(comp.Segments))
	for _, s := range comp.Segments {
		live[s.ID] = s
	}

	// Map each live block to its tool-use partner (if any) so a selected call or
	// result drags the other in atomically.
	partner := pairPartners(proj)

	plan := Plan{Priced: comp.Priced, Currency: comp.Currency}
	chosen := map[string]bool{}
	var order []string // selection order, partners appended right after their block
	var add func(id string, paired bool)
	add = func(id string, paired bool) {
		if chosen[id] {
			return
		}
		if _, ok := live[id]; !ok {
			return // not a live segment (already excluded, or control block)
		}
		chosen[id] = true
		order = append(order, id)
		if !paired {
			if mate, ok := partner[id]; ok {
				add(mate, true)
			}
		}
	}

	// direct records the IDs a selector named, so an item pulled in only to keep a
	// pair atomic is flagged Paired without re-resolving the selectors per item.
	direct := make(map[string]bool, len(selectors))
	for _, sel := range selectors {
		id, ok := resolve(sel, comp.Segments)
		if !ok {
			plan.Unknown = append(plan.Unknown, sel)
			continue
		}
		direct[id] = true
		add(id, false)
	}

	for _, id := range order {
		s := live[id]
		it := Item{ID: id, Kind: s.Kind, Origin: s.Origin, Tokens: s.Tokens, CostUSD: s.CostUSD, Age: s.Age, Paired: !direct[id]}
		plan.Items = append(plan.Items, it)
		plan.Tokens += s.Tokens
		plan.CostUSD += s.CostUSD
	}
	// Largest first: the preview leads with what reclaims the most.
	sort.SliceStable(plan.Items, func(i, j int) bool { return plan.Items[i].Tokens > plan.Items[j].Tokens })

	for _, it := range plan.Items {
		if it.Age >= 0 && it.Age < recentAge {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("%s (%s) is very recent — the current thread may still depend on it", it.Origin, it.Kind))
		}
	}
	return plan
}

// Apply builds the exclusion event that removes the plan's blocks from the
// projection. The returned block is appended to the log by the caller; until
// then nothing is mutated. It returns false for an empty plan (nothing to do).
func Apply(p Plan) (schema.Block, bool) {
	if p.Empty() {
		return schema.Block{}, false
	}
	return eventlog.NewExclusion(ApplyProducer, p.IDs()...), true
}

// Undo finds the most recent still-active `/clean` removal and builds the
// counter-exclusion that reverses it exactly, restoring the blocks it dropped.
// removed is how many blocks the reversed removal had excluded. ok is false when
// there is no removal left to undo. events is the full log (e.g. Log.Events()).
func Undo(events []schema.Block) (event schema.Block, removed int, ok bool) {
	targeted := targetedIDs(events)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != eventlog.KindExclusion || e.Provenance == nil {
			continue
		}
		if e.Provenance.Producer != ApplyProducer {
			continue // skip undos and other producers' exclusions
		}
		if targeted[e.ID] {
			continue // already undone by a later counter-exclusion
		}
		return eventlog.NewExclusion(UndoProducer, e.ID), len(e.Provenance.DerivedFrom), true
	}
	return schema.Block{}, 0, false
}

// targetedIDs returns the set of block IDs named in any event's DerivedFrom — an
// exclusion whose ID is in this set has itself been excluded (i.e. countered).
func targetedIDs(events []schema.Block) map[string]bool {
	out := map[string]bool{}
	for _, e := range events {
		if e.Provenance == nil {
			continue
		}
		for _, id := range e.Provenance.DerivedFrom {
			out[id] = true
		}
	}
	return out
}

// pairPartners maps a live tool-call block to its live tool-result block and
// vice versa, keyed by ToolUseID, so removing one atomically removes the other.
// Only live blocks are linked: a partner already excluded is no orphan risk.
func pairPartners(proj *projection.Projection) map[string]string {
	type pair struct{ call, result string }
	byUse := map[string]*pair{}
	for _, b := range proj.Blocks() {
		if !b.Live {
			continue
		}
		var use string
		isCall := false
		switch {
		case b.ToolCall != nil:
			use, isCall = b.ToolCall.ToolUseID, true
		case b.ToolResult != nil:
			use = b.ToolResult.ToolUseID
		default:
			continue
		}
		if use == "" {
			continue
		}
		p := byUse[use]
		if p == nil {
			p = &pair{}
			byUse[use] = p
		}
		if isCall {
			p.call = b.ID
		} else {
			p.result = b.ID
		}
	}
	out := map[string]string{}
	for _, p := range byUse {
		if p.call != "" && p.result != "" {
			out[p.call] = p.result
			out[p.result] = p.call
		}
	}
	return out
}

// resolve maps a selector to a live segment ID: an exact ID, or any unambiguous
// prefix of one (the "blk_" prefix optional). A prefix matching more than one
// live segment is rejected (ok=false) so an ambiguous handle never removes the
// wrong block.
func resolve(selector string, segs []composition.Segment) (string, bool) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return "", false
	}
	var match string
	n := 0
	for _, s := range segs {
		// An exact ID (with or without the blk_ prefix) wins outright, even when
		// it is also a prefix of a longer ID — otherwise a fully-typed handle that
		// happens to prefix another segment would read as ambiguous.
		if s.ID == sel || s.ID == idPrefix+sel {
			return s.ID, true
		}
		if strings.HasPrefix(s.ID, sel) || strings.HasPrefix(s.ID, idPrefix+sel) {
			match = s.ID
			n++
		}
	}
	if n == 1 {
		return match, true
	}
	return "", false
}

// idPrefix mirrors schema's block-ID prefix so a handle typed without it (the
// natural copy from a short display handle) still resolves.
const idPrefix = "blk_"
