// Package tidy reorganizes a messy context window without lossy summarization —
// the engine behind `/tidy` (AS-043, PRD §7.13, D6), the third flagship wedge.
//
// V1 is the mechanical, zero-token core the ticket scopes as the "clear part":
// dedupe identical file reads. When a file is read more than once into the live
// window only the latest read carries the current content; the older reads are
// pure waste. Tidy keeps the latest read of each path and drops the rest as a
// single appended exclusion event — history is never mutated (D3) — so the
// reclaim is previewable, reversible (`/tidy --undo`), and the surviving reads
// stay ordinary file-read segments a follow-up `/clean` can still target.
//
// The §9 risk row demands tidy never become another lossy compact: the preview
// is a fidelity diff (a before/after inventory, exactly which reads are kept vs
// dropped, the token delta, and a warning when a dropped read is very recent),
// and because dedup only removes older identical reads of a path whose latest
// read is retained, every live fact survives by construction — there is nothing
// to summarize and nothing to lose.
//
// Dead-end collapse (AS-117, §7.13) extends the same reversible exclusion: over
// the live window it surfaces heuristic dead ends — shell commands that failed
// repeatedly and file reads never referenced again — and folds them into the one
// exclusion event dedup already builds, so both share a single preview→apply/undo
// cycle and neither becomes a silent removal path (the fidelity diff still
// decides). Working-memory promotion — the other §7.13 half — is not here: it is
// a memory-file *write*, so it reuses AS-048's single memory-writing path in the
// command layer rather than this exclusion engine (see deadend.go).
package tidy

import (
	"fmt"
	"sort"
	"time"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// Producers attribute the exclusion events tidy appends, kept distinct from
// /clean's so an undo reverses only what tidy removed and a /clean undo never
// reverses a tidy dedup (and vice versa).
const (
	// ApplyProducer attributes the dedup removal exclusion.
	ApplyProducer = "/tidy"
	// UndoProducer attributes the counter-exclusion an undo appends.
	UndoProducer = "/tidy --undo"
)

// recentAge flags a "very recent" read: dropping an older read this fresh is
// more likely a mistake (the live thread may still be working with it), so the
// fidelity diff warns rather than refusing — the user stays in control (PRD §6
// guardrail). It mirrors /clean's recency threshold.
const recentAge = 2 * time.Minute

// Item is one file-read segment a tidy plan touches, with the dimensions the
// fidelity diff shows.
type Item struct {
	ID      string        // stable block handle (the /clean selection key)
	Tokens  int           // estimated window share
	CostUSD float64       // Tokens priced at the active model's input rate
	Age     time.Duration // now − append time
	Seq     int           // append order (recency); the latest read is kept
}

// Group is one deduped file path: the latest read kept and the older reads
// dropped. Tokens/CostUSD are the reclaim — the dropped reads only, never the
// retained one.
type Group struct {
	Path    string
	Keep    Item
	Drop    []Item
	Tokens  int
	CostUSD float64
}

// Plan is the previewed effect of a tidy. Building one mutates nothing; it is
// the fidelity diff the user confirms before any change is applied (§9). The
// Before/After fields are the segment + token inventory the diff reports.
type Plan struct {
	Groups   []Group   // deduped paths, largest reclaim first
	DeadEnds []DeadEnd // heuristic dead ends (AS-117), largest reclaim first
	Tokens   int       // total estimated tokens reclaimed (dedup + dead ends)
	CostUSD  float64   // total cost reclaimed at the input rate
	Priced   bool      // false when the active model is unpriced ($ blank)
	Currency string    // money prefix, e.g. "$"
	Warnings []string

	BeforeSegments int // live segments before tidy
	BeforeTokens   int // live window tokens before tidy
	AfterSegments  int // live segments after the dedup
	AfterTokens    int // live window tokens after the dedup
}

// Empty reports whether the plan would change nothing.
func (p Plan) Empty() bool { return len(p.Groups) == 0 && len(p.DeadEnds) == 0 }

// IDs returns the block IDs the plan drops — the older duplicate reads plus the
// dead-end spans — in plan order. These are the targets of the single exclusion
// event Apply builds, so dedup and dead-end collapse share one apply/undo cycle.
func (p Plan) IDs() []string {
	var out []string
	for _, g := range p.Groups {
		for _, it := range g.Drop {
			out = append(out, it.ID)
		}
	}
	for _, d := range p.DeadEnds {
		for _, it := range d.Drop {
			out = append(out, it.ID)
		}
	}
	return out
}

// DroppedCount is how many reads the plan would drop across all paths.
func (p Plan) DroppedCount() int { return len(p.IDs()) }

// Preview computes the dedup plan over proj's live window: for every file read
// more than once, keep the latest read and drop the earlier ones, pricing the
// reclaim at model's input rate from table and dating reads against now. It is
// pure and never mutates the log, so the fidelity diff opens instantly from log
// data alone (no model calls).
func Preview(proj *projection.Projection, table *cost.Table, model string, now time.Time) Plan {
	// Reuse the composition so the preview's token/$ figures match /context
	// exactly — including its duplicate-file-read detection (AS-026).
	comp := composition.Build(proj, table, model, now, composition.SortSize)
	segByID := make(map[string]composition.Segment, len(comp.Segments))
	for _, s := range comp.Segments {
		segByID[s.ID] = s
	}

	plan := Plan{
		Priced:         comp.Priced,
		Currency:       comp.Currency,
		BeforeSegments: len(comp.Segments),
		BeforeTokens:   comp.TotalTokens,
	}

	for _, d := range comp.Duplicates {
		segs := make([]composition.Segment, 0, len(d.SegmentIDs))
		for _, id := range d.SegmentIDs {
			if s, ok := segByID[id]; ok {
				segs = append(segs, s)
			}
		}
		if len(segs) < 2 {
			continue // not actually duplicated in the live window
		}
		// Keep the latest read (highest append order); drop the rest. Sorting by
		// Seq makes "latest" explicit rather than relying on slice order.
		sort.SliceStable(segs, func(i, j int) bool { return segs[i].Seq < segs[j].Seq })
		keep := segs[len(segs)-1]
		g := Group{Path: d.Path, Keep: itemOf(keep)}
		warned := false // one recency warning per path, however many old reads are fresh
		for _, s := range segs[:len(segs)-1] {
			g.Drop = append(g.Drop, itemOf(s))
			g.Tokens += s.Tokens
			g.CostUSD += s.CostUSD
			if !warned && s.Age >= 0 && s.Age < recentAge {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("an older read of %s is very recent — the current thread may still depend on it", d.Path))
				warned = true
			}
		}
		plan.Groups = append(plan.Groups, g)
		plan.Tokens += g.Tokens
		plan.CostUSD += g.CostUSD
	}

	// Largest reclaim first: the diff leads with the path that frees the most.
	sort.SliceStable(plan.Groups, func(i, j int) bool { return plan.Groups[i].Tokens > plan.Groups[j].Tokens })

	// Dead-end collapse (AS-117) rides the same exclusion event: mark the blocks
	// the dedup already claims so a dead end never double-counts a dropped read,
	// then fold each span's reclaim into the plan total.
	dropped := make(map[string]bool)
	for _, id := range plan.IDs() {
		dropped[id] = true
	}
	plan.DeadEnds = detectDeadEnds(proj.Live(), segByID, dropped)
	for _, d := range plan.DeadEnds {
		plan.Tokens += d.Tokens
		plan.CostUSD += d.CostUSD
	}

	plan.AfterSegments = plan.BeforeSegments - plan.DroppedCount()
	plan.AfterTokens = plan.BeforeTokens - plan.Tokens
	return plan
}

// Apply builds the single exclusion event that drops the plan's older reads
// from the projection. The returned block is appended to the log by the caller;
// until then nothing is mutated. It returns false for an empty plan.
func Apply(p Plan) (schema.Block, bool) {
	if p.Empty() {
		return schema.Block{}, false
	}
	return eventlog.NewExclusion(ApplyProducer, p.IDs()...), true
}

// Undo finds the most recent still-active `/tidy` dedup and builds the
// counter-exclusion that reverses it exactly, restoring the reads it dropped.
// removed is how many reads the reversed dedup had excluded. ok is false when
// there is no tidy removal left to undo. events is the full log.
func Undo(events []schema.Block) (event schema.Block, removed int, ok bool) {
	targeted := targetedIDs(events)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != eventlog.KindExclusion || e.Provenance == nil {
			continue
		}
		if e.Provenance.Producer != ApplyProducer {
			continue // skip undos, /clean exclusions, and other producers
		}
		if targeted[e.ID] {
			continue // already undone by a later counter-exclusion
		}
		return eventlog.NewExclusion(UndoProducer, e.ID), len(e.Provenance.DerivedFrom), true
	}
	return schema.Block{}, 0, false
}

// itemOf reduces a composition segment to the dimensions the fidelity diff
// reports.
func itemOf(s composition.Segment) Item {
	return Item{ID: s.ID, Tokens: s.Tokens, CostUSD: s.CostUSD, Age: s.Age, Seq: s.Seq}
}

// targetedIDs returns the set of block IDs named in any event's DerivedFrom — an
// exclusion whose ID is in this set has itself been excluded (i.e. countered).
// Mirrors clean.targetedIDs so tidy's undo follows the same exact-reversal rule.
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
