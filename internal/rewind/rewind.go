// Package rewind computes and applies conversation rewinds — the engine behind
// `/rewind` (AS-037, PRD §7.16, D3). A checkpoint is just a point in the event
// log; rewinding to it appends a single exclusion event (eventlog.KindExclusion)
// naming every event after that point, so the projection (AS-006) shrinks to the
// state the log held there. History is never rewritten: the rewind is itself an
// appended event, so it is undoable like any other edit (D3) — undo is a further
// exclusion targeting the rewind event.
//
// Because a rewind that names every event at index >= n yields exactly the
// point-in-time projection ProjectAt(events, n) (the later events either drop
// from the window or, when they were themselves control events, stop taking
// effect), rewinding to turn N reproduces the historical projection at turn N
// without copying or mutating anything (the AC golden equality).
//
// Scope (documented, AS-037): a rewind reverts conversation state only.
// File-system changes made by tools after the checkpoint are NOT reverted; the
// preview lists the files those turns modified so the user is warned before
// confirming.
package rewind

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// Producers attribute the events rewind appends, so undo can find the rewinds it
// should reverse without mistaking its own counter-events for rewinds.
const (
	// Producer attributes the exclusion event a rewind appends.
	Producer = "/rewind"
	// UndoProducer attributes the counter-exclusion an undo appends.
	UndoProducer = "/rewind --undo"
	// MarkProducer attributes a manual named checkpoint (`/rewind --mark "…"`).
	MarkProducer = "/rewind --mark"
)

// Checkpoint is a point the conversation can be rewound to: rewinding keeps
// events[:Index] and drops events[Index:]. Automatic checkpoints sit at the
// start of each user turn (the leading user message); a manual checkpoint
// (`/rewind --mark`) sits just after its mark event, so rewinding to it keeps
// the mark and everything before it.
type Checkpoint struct {
	// Index is the event-slice index the rewind keeps up to (exclusive): the
	// projection it restores is ProjectAt(events, Index).
	Index int
	// Anchor is the stable block ID identifying this checkpoint — the user
	// message that begins the turn, or the mark event — used as the picker value
	// and the `/rewind <id>` selector.
	Anchor string
	// Turn is the 1-based user-turn number for an automatic checkpoint, or 0 for
	// a manual mark.
	Turn int
	// Label is the manual mark's text, or the first line of the turn's user
	// message (a preview for the picker).
	Label string
	// Time is the append time of the anchor block.
	Time time.Time
	// Manual is true for a `/rewind --mark` checkpoint.
	Manual bool
}

// Checkpoints returns the rewind points for events in chronological order: one
// at the start of every user-text turn, plus one just after each manual mark.
// The slice is empty when there is nothing to rewind to.
func Checkpoints(events []schema.Block) []Checkpoint {
	var out []Checkpoint
	turn := 0
	for i, e := range events {
		switch {
		case e.Kind == eventlog.KindCheckpoint:
			out = append(out, Checkpoint{
				Index:  i + 1, // keep the mark and everything before it
				Anchor: e.ID,
				Label:  markLabel(e),
				Time:   e.TS,
				Manual: true,
			})
		case e.Role == schema.RoleUser && e.Kind == schema.KindText:
			turn++
			out = append(out, Checkpoint{
				Index:  i, // revert to the state just before this turn's message
				Anchor: e.ID,
				Turn:   turn,
				Label:  firstLine(textOf(e)),
				Time:   e.TS,
			})
		}
	}
	return out
}

// Find returns the checkpoint whose Anchor matches the given block ID (a full ID
// or any unambiguous prefix, the "blk_" prefix optional), mirroring how /context
// handles resolve. ok is false when no checkpoint matches or a prefix is
// ambiguous.
func Find(events []schema.Block, selector string) (Checkpoint, bool) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return Checkpoint{}, false
	}
	cps := Checkpoints(events)
	var match Checkpoint
	n := 0
	for _, c := range cps {
		if c.Anchor == sel || c.Anchor == idPrefix+sel {
			return c, true // a fully-typed handle wins outright
		}
		if strings.HasPrefix(c.Anchor, sel) || strings.HasPrefix(c.Anchor, idPrefix+sel) {
			match = c
			n++
		}
	}
	if n == 1 {
		return match, true
	}
	return Checkpoint{}, false
}

// Plan is the previewed effect of a rewind. Building one mutates nothing; it is
// the preview the user confirms before any change is applied (AC).
type Plan struct {
	Target       Checkpoint
	DropIDs      []string // event IDs the rewind exclusion names (everything at/after Target.Index)
	Blocks       int      // count of live window blocks that would leave the window
	Tokens       int      // estimated tokens of the blocks leaving the window
	CostUSD      float64  // Tokens priced at the active model's input rate
	Returns      int      // blocks that re-enter the window (a later /clean removal is undone)
	ReturnTokens int      // estimated tokens of the returning blocks
	Priced       bool     // false when the active model is unpriced ($ blank)
	Currency     string   // money prefix, e.g. "$"
	Files        []string // files modified by the rewound turns (not reverted — a warning)
}

// Empty reports whether the rewind would drop nothing (already at that point).
func (p Plan) Empty() bool { return len(p.DropIDs) == 0 }

// NetTokens is the net change in the live window: tokens leaving minus tokens
// returning. It is positive when the window shrinks and negative when a rewind
// undoes a later /clean removal and the window grows.
func (p Plan) NetTokens() int { return p.Tokens - p.ReturnTokens }

// Preview computes the effect of rewinding to target: which events the rewind
// exclusion would name, how many live blocks and tokens/$ leave the window, how
// many blocks return to it (a later /clean removal undone by the rewind), and
// which files the rewound turns modified (a warning — files are not reverted).
// It is pure and never mutates the log. events is the full log.
func Preview(events []schema.Block, table *cost.Table, model string, now time.Time, target Checkpoint) Plan {
	n := target.Index
	if n < 0 {
		n = 0
	}
	if n > len(events) {
		n = len(events)
	}

	plan := Plan{Target: target}
	for _, e := range events[n:] {
		plan.DropIDs = append(plan.DropIDs, e.ID)
	}
	if plan.Empty() {
		return plan
	}

	opts := projection.Options{TargetModel: model}
	// The live blocks that survive the rewind are exactly the point-in-time
	// projection's live set.
	postProj := projection.ProjectAt(events, n, opts)
	survives := map[string]bool{}
	for _, b := range postProj.Live() {
		survives[b.ID] = true
	}

	cur := composition.Build(projection.Project(events, opts), table, model, now, composition.SortSize)
	plan.Priced = cur.Priced
	plan.Currency = cur.Currency
	curLive := make(map[string]bool, len(cur.Segments))
	for _, seg := range cur.Segments {
		curLive[seg.ID] = true
		if survives[seg.ID] {
			continue
		}
		// Live now but not after the rewind: this block leaves the window.
		plan.Blocks++
		plan.Tokens += seg.Tokens
		plan.CostUSD += seg.CostUSD
	}

	// A rewind drops every event after the checkpoint, including any later
	// control event — so a /clean exclusion made after the checkpoint is undone
	// and the blocks it had removed return to the window. Count them so the
	// preview reports the net change, not just the blocks leaving (otherwise it
	// would claim tokens "reclaimed" while the window actually grows).
	post := composition.Build(postProj, table, model, now, composition.SortSize)
	for _, seg := range post.Segments {
		if curLive[seg.ID] {
			continue
		}
		plan.Returns++
		plan.ReturnTokens += seg.Tokens
	}

	plan.Files = modifiedFiles(events[n:])
	return plan
}

// Apply builds the exclusion event that rewinds to the plan's checkpoint. The
// returned block is appended to the log by the caller; until then nothing is
// mutated. It returns false for an empty plan (nothing to rewind).
func Apply(p Plan) (schema.Block, bool) {
	if p.Empty() {
		return schema.Block{}, false
	}
	return eventlog.NewExclusion(Producer, p.DropIDs...), true
}

// Undo finds the most recent still-active rewind and builds the
// counter-exclusion that reverses it exactly, restoring everything the rewind
// dropped. ok is false when there is no rewind left to undo. events is the full
// log.
func Undo(events []schema.Block) (event schema.Block, ok bool) {
	targeted := targetedIDs(events)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != eventlog.KindExclusion || e.Provenance == nil {
			continue
		}
		if e.Provenance.Producer != Producer {
			continue // skip undos and other producers' exclusions
		}
		if targeted[e.ID] {
			continue // already undone (or superseded) by a later counter-exclusion
		}
		return eventlog.NewExclusion(UndoProducer, e.ID), true
	}
	return schema.Block{}, false
}

// Mark builds a named manual checkpoint event for the current end of the log.
// The returned block is appended by the caller; rewinding to it later restores
// the state as of the mark (the mark and everything before it stay).
func Mark(label string) schema.Block {
	return eventlog.NewCheckpoint(MarkProducer, strings.TrimSpace(label))
}

// modifiedFiles returns the distinct project-relative paths the given events'
// write/edit tool calls targeted, so the preview can warn that those file
// changes are NOT reverted by a conversation rewind (AS-037 scope decision).
// Shell-driven changes are not parsed; only the structured file tools are.
func modifiedFiles(events []schema.Block) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range events {
		if e.Kind != schema.KindToolCall || e.ToolCall == nil {
			continue
		}
		if e.ToolCall.Name != "write" && e.ToolCall.Name != "edit" {
			continue
		}
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(e.ToolCall.Arguments, &args); err != nil {
			continue
		}
		if args.Path == "" || seen[args.Path] {
			continue
		}
		seen[args.Path] = true
		out = append(out, args.Path)
	}
	sort.Strings(out)
	return out
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

// textOf returns a block's text body, or "".
func textOf(b schema.Block) string {
	if b.Text == nil {
		return ""
	}
	return b.Text.Text
}

// markLabel returns a mark event's label, falling back to a generic name.
func markLabel(b schema.Block) string {
	if s := strings.TrimSpace(textOf(b)); s != "" {
		return s
	}
	return "(unnamed mark)"
}

// firstLine returns the first non-empty line of s, trimmed, as a preview.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}

// idPrefix mirrors schema's block-ID prefix so a checkpoint handle typed without
// it still resolves (matching /clean's selector behavior).
const idPrefix = "blk_"

// RenderPreview formats a plan as the confirmation text the face shows before a
// rewind is applied: where it rewinds to, what leaves the window, and the
// files-not-reverted warning.
func RenderPreview(p Plan) string {
	if p.Empty() {
		return "Nothing to rewind — already at that point."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Rewind to %s\n\n", p.Target.describe())
	fmt.Fprintf(&b, "  %s · ~%d tokens", segmentsLabel(p.Blocks), p.Tokens)
	if p.Priced {
		fmt.Fprintf(&b, " (%s%s)", p.Currency, strconv.FormatFloat(p.CostUSD, 'f', 4, 64))
	}
	b.WriteString(" leave the window.\n")
	if p.Returns > 0 {
		fmt.Fprintf(&b, "  ⚠ %s · ~%d tokens return to the window — this rewind undoes a later /clean removal.\n",
			segmentsLabel(p.Returns), p.ReturnTokens)
		fmt.Fprintf(&b, "  Net change: ~%d tokens (%s).\n", p.NetTokens(), netDirection(p.NetTokens()))
	}
	if len(p.Files) > 0 {
		b.WriteString("\n⚠ File changes are NOT reverted. Files modified after this point:\n")
		for _, f := range p.Files {
			fmt.Fprintf(&b, "    %s\n", f)
		}
	}
	b.WriteString("\nNothing leaves the log — confirm with /rewind --apply, or /rewind --cancel.\n")
	b.WriteString("The rewind is itself reversible with /rewind --undo.")
	return b.String()
}

// describe labels a checkpoint for the preview/picker.
func (c Checkpoint) describe() string {
	if c.Manual {
		return fmt.Sprintf("mark %q (%s)", c.Label, ShortAnchor(c.Anchor))
	}
	return fmt.Sprintf("turn %d: %q (%s)", c.Turn, clip(c.Label, 48), ShortAnchor(c.Anchor))
}

// ShortAnchor trims a block ID to a compact handle for display, dropping the
// "blk_" prefix so it reads like the /context handles. It is the handle the
// /rewind listing shows and `/rewind <handle>` accepts.
func ShortAnchor(id string) string {
	h := strings.TrimPrefix(id, idPrefix)
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// segmentsLabel pluralizes a block count for the preview.
func segmentsLabel(n int) string {
	if n == 1 {
		return "1 block"
	}
	return strconv.Itoa(n) + " blocks"
}

// netDirection labels a net token change for the preview: a positive net shrinks
// the window, a negative net grows it.
func netDirection(net int) string {
	if net < 0 {
		return "window grows"
	}
	return "window shrinks"
}

// clip shortens s to at most n runes, ending in an ellipsis when it was longer.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
