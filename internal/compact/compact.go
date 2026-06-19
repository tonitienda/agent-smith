// Package compact computes and applies `/compact` (AS-038, PRD §7.16, Appendix
// A, D3): the blunt-instrument fallback that summarizes the older conversation
// into a single derived block when `/clean` and `/tidy` are not enough. Unlike
// every incumbent's destructive compaction, ours is reversible — that is the
// whole point of D3. The summary is a derived compaction block
// (schema.KindCompaction) whose source blocks are excluded but kept on the log,
// with provenance linking the summary back to every source ID. Because a derived
// block excludes its sources through the same Provenance.DerivedFrom mechanism
// an exclusion uses (see internal/projection), applying a compaction is a single
// appended event, and `/compact --undo` is a counter-exclusion targeting that
// block — restoring the exact pre-compact projection.
//
// This package is pure: it selects the compactable span and builds the events,
// but the summarization model call (cheap tier, AS-038 AC4) is I/O the caller
// performs, handing the resulting text to Build.
package compact

import (
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// Producers attribute the events compact appends, so an undo can find the
// compaction it should reverse without mistaking its own counter-event for one.
const (
	// Producer attributes the derived compaction block. The auto-compaction path
	// (AS-085) reuses it for the block too, so /compact --undo finds and reverses
	// an auto-compaction exactly as it does a manual one.
	Producer = "/compact"
	// UndoProducer attributes the counter-exclusion an undo appends.
	UndoProducer = "/compact --undo"
	// AutoUsageProducer attributes the summarization usage event of an
	// auto-compaction (AS-085), distinct from the manual /compact usage producer
	// so /cost itemizes both and /insights can tell auto from user-invoked spend.
	// Only the usage event differs; the compaction block keeps Producer so undo
	// works uniformly.
	AutoUsageProducer = "/compact (auto)"
)

// header prefixes the summary text so the model reads the compaction block for
// what it is — a stand-in for the conversation that preceded it — rather than as
// a fresh user message.
const header = "[Summary of earlier conversation, compacted to save context]\n\n"

// Plan is the previewed effect of a compaction: the live blocks that would be
// summarized away and the window share they hold. Building one mutates nothing;
// it is the preview the user confirms (AC: tokens before/after, what's being
// summarized) and the input the summarizer reads.
type Plan struct {
	SourceIDs    []string       // blocks to summarize and exclude, in append order
	Sources      []schema.Block // the same blocks, for rendering the summarizer's transcript
	SourceTokens []int          // each source's estimated window share, aligned with SourceIDs
	Tokens       int            // total estimated window tokens the sources hold
	CostUSD      float64        // those tokens priced at the active model's input rate
	Priced       bool           // false when the active model is unpriced ($ blank)
	Currency     string         // money prefix, e.g. "$"
}

// Empty reports whether the plan would compact nothing.
func (p Plan) Empty() bool { return len(p.SourceIDs) == 0 }

// Preview selects the compactable span from proj's live window: every live
// content block except the system/memory prefix (kept so the agent's standing
// instructions survive) and the most recent turn (kept so the live thread keeps
// working). It computes the tokens/$ those sources hold from the same
// composition `/context` shows, so the preview figures never drift. It is pure
// and never mutates the log.
func Preview(proj *projection.Projection, table *cost.Table, model string, now time.Time) Plan {
	// Reuse the composition so the token/$ figures match /context exactly.
	comp := composition.Build(proj, table, model, now, composition.SortSize)
	seg := make(map[string]composition.Segment, len(comp.Segments))
	for _, s := range comp.Segments {
		seg[s.ID] = s
	}

	blocks := proj.Blocks()
	recent := recentStart(blocks)

	plan := Plan{Priced: comp.Priced, Currency: comp.Currency}
	for i := 0; i < recent; i++ {
		b := blocks[i]
		if !b.Live || !compactable(b.Block) {
			continue
		}
		s, ok := seg[b.ID]
		if !ok {
			continue // not a live window segment (e.g. a reasoning block dropped by replay scope)
		}
		plan.SourceIDs = append(plan.SourceIDs, b.ID)
		plan.Sources = append(plan.Sources, b.Block)
		plan.SourceTokens = append(plan.SourceTokens, s.Tokens)
		plan.Tokens += s.Tokens
		plan.CostUSD += s.CostUSD
	}
	return plan
}

// Build assembles the derived compaction block from the plan and the summary the
// cheap-tier model produced. The returned block both renders the summary into
// the window and excludes the plan's source blocks (eventlog.Derive stamps
// Provenance.DerivedFrom with every source ID), so applying it is one appended
// event. ok is false for an empty plan or an empty summary — nothing to record.
func Build(p Plan, summary string) (schema.Block, bool) {
	summary = strings.TrimSpace(summary)
	if p.Empty() || summary == "" {
		return schema.Block{}, false
	}
	b := schema.Block{
		Kind: schema.KindCompaction,
		Role: schema.RoleUser,
		Text: &schema.TextBody{Text: header + summary, Subtype: schema.TextSubtypeNormal},
	}
	return eventlog.Derive(b, Producer, p.SourceIDs...), true
}

// Undo finds the most recent still-active `/compact` block and builds the
// counter-exclusion that reverses it exactly: excluding the compaction block
// removes the summary from the window and, because the block is now itself
// excluded, cancels its exclusion of the sources — so they return. removed is
// how many source blocks that compaction had folded away. ok is false when there
// is no compaction left to undo. events is the full log (e.g. Log.Events()).
func Undo(events []schema.Block) (event schema.Block, removed int, ok bool) {
	targeted := targetedIDs(events)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != schema.KindCompaction || e.Provenance == nil {
			continue
		}
		if e.Provenance.Producer != Producer {
			continue // skip compaction blocks from other producers
		}
		if targeted[e.ID] {
			continue // already undone by a later counter-exclusion
		}
		return eventlog.NewExclusion(UndoProducer, e.ID), len(e.Provenance.DerivedFrom), true
	}
	return schema.Block{}, 0, false
}

// Transcript renders the source blocks as a plain-text conversation transcript
// for the summarizer. Rendering to text — rather than replaying the blocks as
// chat turns — keeps the summarization request identical on every provider and
// sidesteps tool-call/result pairing constraints the live conversation imposes.
func Transcript(sources []schema.Block) string {
	var b strings.Builder
	for _, s := range sources {
		if line := lineFor(s); line != "" {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// lineFor renders one source block as a single transcript line, or "" for a
// block carrying nothing worth summarizing.
func lineFor(b schema.Block) string {
	switch b.Kind {
	case schema.KindText:
		if b.Text == nil || strings.TrimSpace(b.Text.Text) == "" {
			return ""
		}
		return string(b.Role) + ": " + b.Text.Text
	case schema.KindReasoning:
		if b.Reasoning == nil || strings.TrimSpace(b.Reasoning.Text) == "" {
			return ""
		}
		return "assistant (reasoning): " + b.Reasoning.Text
	case schema.KindToolCall:
		if b.ToolCall == nil {
			return ""
		}
		return "tool call: " + b.ToolCall.Name
	case schema.KindToolResult:
		if b.ToolResult == nil {
			return ""
		}
		return "tool result: " + resultText(b.ToolResult)
	case schema.KindFileRead:
		if b.FileRead == nil {
			return ""
		}
		return "file read: " + b.FileRead.Path
	default:
		return ""
	}
}

// resultText extracts a compact textual rendering of a tool result for the
// transcript: stdout when present, otherwise the concatenated text parts.
func resultText(r *schema.ToolResultBody) string {
	if r.Stdout != "" {
		return r.Stdout
	}
	var b strings.Builder
	for _, p := range r.Content {
		if p.Text != "" {
			b.WriteString(p.Text)
		}
	}
	if b.Len() == 0 {
		return "(no textual output)"
	}
	return b.String()
}

// recentStart returns the index in blocks of the last live user-text block: the
// start of the current turn, which compaction keeps so the live thread stays
// intact. Everything before it is compactable; everything at or after it is the
// preserved recent tail. When there is no live user turn, the whole window is
// eligible.
func recentStart(blocks []projection.Block) int {
	for i := len(blocks) - 1; i >= 0; i-- {
		b := blocks[i]
		if b.Live && b.Kind == schema.KindText && b.Role == schema.RoleUser {
			return i
		}
	}
	return len(blocks)
}

// compactable reports whether a block belongs in the compactable span: a content
// block from the conversation, never a system/memory or harness block (kept so
// standing instructions survive) and never a prior compaction summary (so a
// second /compact summarizes only the conversation since the last one, not the
// summary itself).
func compactable(b schema.Block) bool {
	if b.Role == schema.RoleSystem || b.Role == schema.RoleHarness {
		return false
	}
	switch b.Kind {
	case schema.KindText, schema.KindToolCall, schema.KindToolResult, schema.KindFileRead, schema.KindReasoning:
		return true
	default:
		return false
	}
}

// targetedIDs returns the set of block IDs named in any event's DerivedFrom — a
// compaction whose ID is in this set has itself been excluded (i.e. undone).
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
