// Package goal implements the /goal session-objective command (AS-040). A goal
// is a small, model-facing text block appended to the event log (PRD D3): the
// model reads it as a standing objective, it persists in the status line, and
// its whole history is reconstructable from the log alone — so /insights
// (AS-045) reads the goal straight from the session's events, with no separate
// stored state (the session Metadata deliberately holds only fields that are
// NOT reconstructible from the stream).
//
// Setting, replacing, and completing a goal are all appended events, never
// mutations:
//
//   - Set appends a goal text block ("Session goal: <objective>").
//   - Replacing or completing the goal appends an exclusion (eventlog) that
//     retires the previously active goal block, so exactly one goal is live in
//     the window at a time while the full history stays on the log and in the
//     /context archive.
package goal

import (
	"fmt"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// Producer attributes the events this command appends — the goal block and the
// exclusions that retire earlier goals — so the goal history is identifiable on
// the log without spending a frozen content kind on it.
const Producer = "/goal"

// textPrefix prefixes the goal block's rendered text so the model reads it as a
// standing objective. objective strips it back off.
const textPrefix = "Session goal: "

// Set builds the goal block for objective: a model-facing system text block
// attributed to this command. It is appended to the log as-is; its Seq and
// timestamp are assigned at append time. Callers retire any currently active
// goal with Retire first, so only one goal is live at a time.
func Set(objective string) schema.Block {
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       schema.KindText,
		Role:       schema.RoleSystem,
		Text:       &schema.TextBody{Text: textPrefix + objective},
		Provenance: &schema.Provenance{Producer: Producer},
	}
}

// Retire builds the exclusion event that drops the goal block with id from the
// projection (D3: history is untouched; the block stays on the log and in the
// /context archive). It is appended both when replacing the goal and on
// `/goal done`.
func Retire(id string) schema.Block {
	return eventlog.NewExclusion(Producer, id)
}

// State is one goal in the session's history.
type State struct {
	BlockID   string    // the goal block's stable ID
	Objective string    // the objective text (prefix stripped)
	SetAt     time.Time // when the goal block was appended
	Active    bool      // true while the goal block is still live (not retired)
}

// isGoalBlock reports whether b is a goal-setting block this command appended.
func isGoalBlock(b schema.Block) bool {
	return b.Kind == schema.KindText && b.Text != nil &&
		b.Provenance != nil && b.Provenance.Producer == Producer
}

// objective strips the standing-objective prefix from a goal block's text.
func objective(b schema.Block) string {
	return strings.TrimPrefix(b.Text.Text, textPrefix)
}

// History returns every goal the session has set, in append order, each flagged
// Active when its block is still live in the projection. It is derived purely
// from events — the source AS-045 reads to frame the retro.
func History(events []schema.Block) []State {
	proj := projection.Project(events, projection.Options{})
	var out []State
	for _, b := range proj.Blocks() {
		if !isGoalBlock(b.Block) {
			continue
		}
		out = append(out, State{
			BlockID:   b.ID,
			Objective: objective(b.Block),
			SetAt:     b.TS,
			Active:    b.Live,
		})
	}
	return out
}

// Current returns the session's active goal — the most recent goal block still
// live in the projection — and whether one is set.
func Current(events []schema.Block) (State, bool) {
	hist := History(events)
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Active {
			return hist[i], true
		}
	}
	return State{}, false
}

// Render formats the current goal and its history for `/goal` with no
// arguments. An active goal is marked "→"; retired ones (replaced or completed)
// are marked "·".
func Render(events []schema.Block) string {
	hist := History(events)
	if len(hist) == 0 {
		return `No goal set. Use /goal "<objective>" to set one.`
	}
	var b strings.Builder
	if cur, ok := Current(events); ok {
		fmt.Fprintf(&b, "Current goal: %s\n", cur.Objective)
	} else {
		b.WriteString("No active goal (the last one was completed).\n")
	}
	b.WriteString("\nHistory:\n")
	for _, s := range hist {
		marker := "·"
		if s.Active {
			marker = "→"
		}
		fmt.Fprintf(&b, "  %s %s  %s\n", marker, s.SetAt.Format("2006-01-02 15:04"), s.Objective)
	}
	return b.String()
}
