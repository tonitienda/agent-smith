// Package mode implements the Coding Mode shell (AS-072): an opinionated, soft,
// process-driven working mode that guides a feature through a sequence of phases
// (think → analyse → plan → implement → verify → refactor → reflect). It is the
// thin lifecycle core (coding-mode.prd.md D-CODE-1) — no new engine; it composes
// existing primitives and records mode state on the append-only event log.
//
// Mode state is never mutable side-state (PRD D3, D-CODE-3): entering a mode,
// every phase transition, and exiting are all appended control events
// (eventlog.KindModeEnter / KindPhaseChange / KindModeExit). The "current phase"
// is a projection over those events — reconstructable from the log alone, and
// auditable/reversible for free. New event types are additive-only (PRD D2).
//
// The mode is a soft advisor, never a gate (D-CODE-2): the phase list is data
// (DefaultPhases), the user may jump to any phase at any time, and no phase is a
// precondition for another. The phase list is deliberately a value rather than
// hardcoded control flow so the process skill pack (AS-074) and project memory
// (AS-075) can later override it additively.
package mode

import (
	"fmt"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// Producer attributes the mode lifecycle events this package appends, so mode
// history is identifiable on the log.
const Producer = "/mode"

// Coding is the only mode shipped today (D-CODE-1); the primitive is generic so
// future modes (a review or debug mode) can reuse the entry/exit and phase
// mechanics.
const Coding = "coding"

// DefaultPhases returns the baked-in house method (D-CODE-5.1): the default
// phase order and stance. It is data, not control flow, so AS-074/AS-075 can
// reorder, skip, or extend it without touching the lifecycle core. A fresh slice
// is returned on each call so the default can never be mutated in place by a
// caller — overrides are passed explicitly (every phase-aware function takes a
// phases argument), never by editing a shared global.
func DefaultPhases() []string {
	return []string{"think", "analyse", "plan", "implement", "verify", "refactor", "reflect"}
}

// phaseSkills declares which process skills (AS-074) belong to each phase. The
// phase definitions own this mapping (coding-mode.prd.md D-CODE-5.2) — the names
// are bundled, auto-enabled skills the face loads itself when the phase is active
// (the bodies live in internal/codingskills; this layer stays string-only so the
// lifecycle core depends on no skill content). It is data, not control flow, so a
// project can shadow a named skill (AS-075) without touching the core, and the
// list is extended additively (PRD D2). A phase absent here simply auto-loads no
// skills.
var phaseSkills = map[string][]string{
	"analyse": {"grill-gaps", "find-side-effects"},
	"plan":    {"plan-review"},
	"verify":  {"verify-checklist"},
	"reflect": {"reflect-notes"},
}

// PhaseSkills returns the names of the process skills auto-enabled for phase
// (case-insensitive), or nil when the phase declares none. A fresh slice is
// returned so a caller can never mutate the shared mapping in place.
func PhaseSkills(phase string) []string {
	// The map keys are canonical lowercase phase names, so a lowercased lookup is
	// an O(1) case-insensitive match without scanning the map.
	if names, ok := phaseSkills[strings.ToLower(phase)]; ok {
		return append([]string(nil), names...)
	}
	return nil
}

// State is one Coding Mode instance derived from the log.
type State struct {
	// InstanceID is the mode_enter block's ID — the stable handle phase-change
	// and exit events reference.
	InstanceID string
	// Mode is the mode name (e.g. "coding").
	Mode string
	// Phase is the current phase, projected as the latest phase-change for this
	// instance.
	Phase string
	// EnteredAt is when the mode was entered.
	EnteredAt time.Time
	// Active is true while the instance has not been exited.
	Active bool
}

// Enter builds the events that enter modeName at the first phase of phases: a
// mode_enter and its initial phase-change. The caller appends both in order; the
// initial phase is just the first phase-change, so derivation is uniform (there
// is always at least one phase-change per instance). When phases is empty the
// initial phase is left blank.
func Enter(modeName string, phases []string) []schema.Block {
	enter := eventlog.NewModeEnter(Producer, modeName)
	start := ""
	if len(phases) > 0 {
		start = phases[0]
	}
	return []schema.Block{enter, eventlog.NewPhaseChange(Producer, enter.ID, start)}
}

// SetPhase builds a phase-change event moving the instance to phase.
func SetPhase(instanceID, phase string) schema.Block {
	return eventlog.NewPhaseChange(Producer, instanceID, phase)
}

// Exit builds a mode-exit event ending the instance. Phase history stays on the
// log.
func Exit(instanceID string) schema.Block {
	return eventlog.NewModeExit(Producer, instanceID)
}

// Current returns the active mode instance — the most recent mode_enter that has
// not been exited — and whether one is active. Only one instance is active at a
// time (V1; D-CODE clarified decision), so the latest enter governs.
func Current(events []schema.Block) (State, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != eventlog.KindModeEnter {
			continue
		}
		enter := events[i]
		if exited(events, i, enter.ID) {
			return State{}, false
		}
		return State{
			InstanceID: enter.ID,
			Mode:       blockText(enter),
			Phase:      currentPhase(events, i, enter.ID),
			EnteredAt:  enter.TS,
			Active:     true,
		}, true
	}
	return State{}, false
}

// History returns every mode instance the session has entered, in append order,
// each flagged Active when it has not been exited. It is derived purely from
// events.
func History(events []schema.Block) []State {
	var out []State
	for i, b := range events {
		if b.Kind != eventlog.KindModeEnter {
			continue
		}
		out = append(out, State{
			InstanceID: b.ID,
			Mode:       blockText(b),
			Phase:      currentPhase(events, i, b.ID),
			EnteredAt:  b.TS,
			Active:     !exited(events, i, b.ID),
		})
	}
	return out
}

// exited reports whether a mode-exit referencing instanceID was appended after
// enterIdx.
func exited(events []schema.Block, enterIdx int, instanceID string) bool {
	for j := enterIdx + 1; j < len(events); j++ {
		if events[j].Kind == eventlog.KindModeExit && refersTo(events[j], instanceID) {
			return true
		}
	}
	return false
}

// currentPhase returns the latest phase recorded for the instance after
// enterIdx, scanning in reverse so the most recent phase-change wins.
func currentPhase(events []schema.Block, enterIdx int, instanceID string) string {
	for j := len(events) - 1; j > enterIdx; j-- {
		if events[j].Kind == eventlog.KindPhaseChange && refersTo(events[j], instanceID) {
			return blockText(events[j])
		}
	}
	return ""
}

// refersTo reports whether b names id in Provenance.DerivedFrom.
func refersTo(b schema.Block, id string) bool {
	if b.Provenance == nil {
		return false
	}
	for _, d := range b.Provenance.DerivedFrom {
		if d == id {
			return true
		}
	}
	return false
}

// blockText returns a block's text body, or "" when absent.
func blockText(b schema.Block) string {
	if b.Text == nil {
		return ""
	}
	return b.Text.Text
}

// PhaseIndex returns the position of phase in phases (case-insensitive), or -1.
func PhaseIndex(phases []string, phase string) int {
	for i, p := range phases {
		if strings.EqualFold(p, phase) {
			return i
		}
	}
	return -1
}

// CanonicalPhase resolves phase against phases case-insensitively, returning the
// canonical spelling and whether it is a known phase. Unknown phases are not an
// error the mode enforces (D-CODE-2) — but the commands use this to reject typos
// so a slip does not silently create a junk phase.
func CanonicalPhase(phases []string, phase string) (string, bool) {
	if i := PhaseIndex(phases, phase); i >= 0 {
		return phases[i], true
	}
	return "", false
}

// NextPhase returns the phase after current, or false at the last phase.
func NextPhase(phases []string, current string) (string, bool) {
	i := PhaseIndex(phases, current)
	if i < 0 || i+1 >= len(phases) {
		return "", false
	}
	return phases[i+1], true
}

// PrevPhase returns the phase before current, or false at the first phase.
func PrevPhase(phases []string, current string) (string, bool) {
	i := PhaseIndex(phases, current)
	if i <= 0 {
		return "", false
	}
	return phases[i-1], true
}

// Tracker renders a one-line phase tracker with current bracketed, e.g.
// "think · [analyse] · plan · …". The richer pinned panel is AS-073; this is the
// plain text faces and headless render (D-CODE-4: flavor lives only in the TUI).
func Tracker(phases []string, current string) string {
	parts := make([]string, len(phases))
	for i, p := range phases {
		if strings.EqualFold(p, current) {
			parts[i] = "[" + p + "]"
		} else {
			parts[i] = p
		}
	}
	return strings.Join(parts, " · ")
}

// Render formats the active mode for `/mode` (and `/phase`) with no arguments.
// phases is the active mode's phase list (the tracker row), passed in rather than
// assumed so a future generic mode renders its own phases, not coding's.
func Render(events []schema.Block, phases []string) string {
	cur, ok := Current(events)
	if !ok {
		return `No coding mode active. Use /feature "<prompt>" or /mode coding to enter.`
	}
	return fmt.Sprintf("Mode: %s · phase: %s\n%s", cur.Mode, cur.Phase, Tracker(phases, cur.Phase))
}

// PhaseHistory returns the phases the instance has moved through, in append
// order, derived purely from its phase-change events. Repeats are kept (a user
// may jump back), so the trail reflects what actually happened. Blank phases
// (e.g. an empty initial phase) are skipped.
func PhaseHistory(events []schema.Block, instanceID string) []string {
	enterIdx := -1
	for i, b := range events {
		if b.Kind == eventlog.KindModeEnter && b.ID == instanceID {
			enterIdx = i
			break
		}
	}
	if enterIdx < 0 {
		return nil
	}
	var out []string
	for j := enterIdx + 1; j < len(events); j++ {
		if events[j].Kind == eventlog.KindPhaseChange && refersTo(events[j], instanceID) {
			if p := blockText(events[j]); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// Panel renders the richer, pinned mode view the TUI shows on demand (AS-073):
// the mode, its goal, the phase tracker, and the trail of phases visited so far.
// Like Render it is plain text — flavor and layout live in the face (D-CODE-4) —
// so headless callers can reuse it verbatim. goal is the active session
// objective (AS-040), passed in so this package does not reach into goal state;
// empty is simply omitted. Phase-produced artifacts (AS-076) attach here once
// that ticket records them on the log.
func Panel(events []schema.Block, phases []string, goal string) string {
	cur, ok := Current(events)
	if !ok {
		return `No coding mode active. Use /feature "<prompt>" or /mode coding to enter.`
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Mode: %s · phase: %s\n", cur.Mode, cur.Phase)
	if g := strings.TrimSpace(goal); g != "" {
		fmt.Fprintf(&b, "Goal: %s\n", g)
	}
	fmt.Fprintf(&b, "\nPhases:\n  %s\n", Tracker(phases, cur.Phase))
	if hist := PhaseHistory(events, cur.InstanceID); len(hist) > 0 {
		fmt.Fprintf(&b, "\nVisited:\n  %s\n", strings.Join(hist, " → "))
	}
	return b.String()
}
