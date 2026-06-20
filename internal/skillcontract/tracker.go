package skillcontract

import (
	"strings"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// Actuals is the measured trace accumulated for one span: the counts AS-049
// compares against the contract's EffortBudget. Every field is a soft
// measurement, never a gate.
type Actuals struct {
	ToolCalls int
	Turns     int
	CostUSD   float64
}

// Teardown reasons recorded on a closed Span.
const (
	// TeardownSignal: the declared completion.signal was observed (preferred).
	TeardownSignal = "signal"
	// TeardownIdle: the idle_turns heuristic fired (v1 fallback).
	TeardownIdle = "idle"
	// TeardownSessionEnd: Finish closed a span still open at session end.
	TeardownSessionEnd = "session_end"
)

// Span is one skill activation tracked from activation to teardown: the contract
// it was loaded with (zero when none was declared), the actuals measured over its
// lifetime, and — once closed — how its teardown fired (TeardownSignal,
// TeardownIdle, or TeardownSessionEnd). An Open span has TornDownBy == "".
type Span struct {
	Skill      string
	Contract   Contract
	Actuals    Actuals
	Open       bool
	TornDownBy string

	idle       int  // consecutive turns with no skill tool use
	usedInTurn bool // skill produced a block since the last turn boundary
}

// Tracker follows skill activations across the event log and closes each span at
// the right moment. A consumer declares the contracts for the skills loaded this
// session (Declare), then feeds every appended block in order (Observe); closed
// spans are read back with Closed, and any spans still open at session end are
// flushed with Finish.
//
// Span lifetime:
//   - A span opens when the first block attributed to a skill (Attribution.Skill,
//     other than the skill-load availability marker) is observed; the contract is
//     taken from Declare, or zero if none was declared.
//   - It closes when the declared completion.signal substring is observed in a
//     block (TeardownSignal, preferred), else after Completion.IdleTurns turns
//     elapse with no block attributed to the skill (TeardownIdle), else at Finish
//     (TeardownSessionEnd). IdleTurns == 0 disables the heuristic.
//   - Re-activating a skill after its span closed opens a fresh span, so a skill
//     used twice yields two spans.
//
// Attribution rule for overlapping activations: a block tagged with
// Attribution.Skill accrues to that skill's open span regardless of nesting; an
// untagged block's actuals (tool calls, cost, and the turn boundary) accrue to
// the innermost (most-recently-opened) open span only, so overlapping spans never
// double-count shared cost. Idle accounting is per span: every open span
// independently tracks whether its own skill was used in the current turn.
type Tracker struct {
	contracts map[string]Contract
	open      []*Span // stack: open[len-1] is the innermost (most-recently-opened) span
	byName    map[string]*Span
	closed    []*Span
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{
		contracts: map[string]Contract{},
		byName:    map[string]*Span{},
	}
}

// Declare records the contract a loaded skill carries, so the span opened on its
// activation is tracked against it. Declaring a skill with a zero Contract is
// fine — it simply has no budget or completion trigger. Re-declaring replaces the
// prior contract (it takes effect for the next span opened).
func (t *Tracker) Declare(skill string, c Contract) {
	t.contracts[skill] = c
}

// Observe drives the tracker with one appended block, in log order. It opens and
// closes spans, accumulates actuals, and advances idle accounting on turn
// boundaries (a block carrying a StopReason).
func (t *Tracker) Observe(b schema.Block) {
	sk := ""
	if b.Attribution != nil {
		sk = b.Attribution.Skill
	}

	// A skill-attributed block (other than the availability marker) activates the
	// skill's span if one is not already open, and marks it used this turn.
	if sk != "" && b.Kind != eventlog.KindSkillLoad {
		s := t.byName[sk]
		if s == nil {
			s = t.openSpan(sk)
		}
		s.usedInTurn = true
		t.accrue(s, b)
	} else if inner := t.innermost(); inner != nil {
		// Untagged block: its actuals accrue to the innermost open span.
		t.accrue(inner, b)
	}

	// Declared signal closes its span (preferred trigger); checked after the
	// emitting block's actuals are counted so the closing block still belongs to
	// the span.
	if text := blockText(b); text != "" {
		for _, s := range t.openSnapshot() {
			if sig := s.Contract.Completion.Signal; sig != "" && strings.Contains(text, sig) {
				t.close(s, TeardownSignal)
			}
		}
	}

	// A turn boundary advances idle accounting and the per-span turn count.
	if b.StopReason != "" {
		t.endTurn()
	}
}

// accrue adds one block's measured actuals to a span.
func (t *Tracker) accrue(s *Span, b schema.Block) {
	if b.Kind == schema.KindToolCall {
		s.Actuals.ToolCalls++
	}
	if b.CostUSD != nil {
		s.Actuals.CostUSD += *b.CostUSD
	}
}

// endTurn closes the turn: the innermost open span counts the turn, and every
// open span resets its used-this-turn flag, incrementing the idle counter when
// its skill went untouched and firing the idle-turns teardown when the heuristic
// trips.
func (t *Tracker) endTurn() {
	if inner := t.innermost(); inner != nil {
		inner.Actuals.Turns++
	}
	for _, s := range t.openSnapshot() {
		if s.usedInTurn {
			s.idle = 0
		} else {
			s.idle++
		}
		s.usedInTurn = false
		if n := s.Contract.Completion.IdleTurns; n > 0 && s.idle >= n {
			t.close(s, TeardownIdle)
		}
	}
}

// Finish closes every still-open span as ended at session end and returns the
// full set of closed spans (in close order). It is idempotent: a second call
// returns the same set and opens nothing.
func (t *Tracker) Finish() []Span {
	for _, s := range t.openSnapshot() {
		t.close(s, TeardownSessionEnd)
	}
	return t.Closed()
}

// Closed returns the spans that have been torn down, in the order they closed.
func (t *Tracker) Closed() []Span {
	out := make([]Span, len(t.closed))
	for i, s := range t.closed {
		out[i] = *s
	}
	return out
}

// Open returns the spans still being tracked, innermost last.
func (t *Tracker) Open() []Span {
	out := make([]Span, len(t.open))
	for i, s := range t.open {
		out[i] = *s
	}
	return out
}

// openSpan starts and registers a new span for a skill.
func (t *Tracker) openSpan(skill string) *Span {
	s := &Span{Skill: skill, Contract: t.contracts[skill], Open: true}
	t.open = append(t.open, s)
	t.byName[skill] = s
	return s
}

// innermost returns the most-recently-opened open span, or nil when none is open.
func (t *Tracker) innermost() *Span {
	if len(t.open) == 0 {
		return nil
	}
	return t.open[len(t.open)-1]
}

// openSnapshot copies the open-span slice so the caller can close spans while
// iterating without mutating the slice mid-walk.
func (t *Tracker) openSnapshot() []*Span {
	return append([]*Span(nil), t.open...)
}

// close tears a span down, recording the reason and moving it from the open stack
// to the closed list. A span already closed is left untouched (a signal and the
// session-end flush cannot double-close it).
func (t *Tracker) close(s *Span, reason string) {
	if !s.Open {
		return
	}
	s.Open = false
	s.TornDownBy = reason
	if t.byName[s.Skill] == s {
		delete(t.byName, s.Skill)
	}
	for i, o := range t.open {
		if o == s {
			t.open = append(t.open[:i], t.open[i+1:]...)
			break
		}
	}
	t.closed = append(t.closed, s)
}

// blockText gathers the human-readable text of a block for signal matching: an
// assistant/user text body and a tool result's stdout, stderr, and content parts.
func blockText(b schema.Block) string {
	var sb strings.Builder
	if b.Text != nil {
		sb.WriteString(b.Text.Text)
	}
	if r := b.ToolResult; r != nil {
		sb.WriteByte('\n')
		sb.WriteString(r.Stdout)
		sb.WriteByte('\n')
		sb.WriteString(r.Stderr)
		for _, p := range r.Content {
			if p.Text != "" {
				sb.WriteByte('\n')
				sb.WriteString(p.Text)
			}
		}
	}
	return sb.String()
}
