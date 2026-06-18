// Package budget enforces per-session (and per-task) dollar ceilings for Agent
// Smith (AS-041, PRD §7.15): a session may carry a spend limit that first warns
// near the ceiling and then hard-stops before exceeding it, so a runaway loop or
// an expensive model can never silently burn past a user's budget. No incumbent
// has this — it's a wedge-3 differentiator and the enforcement substrate the
// async runner (AS-054) and sub-agent budget caps (AS-044/046) reuse.
//
// The ceiling is recorded on the append-only log as a budget event (eventlog.
// KindBudget), never as side state, so it survives save/resume and reconciles
// from the log alone — the same derive-from-the-log discipline as cost
// accounting (AS-020). This package owns two things: the durable ceiling on the
// log (Set / Current) and the pure enforcement decision a spend total maps to
// (Guard.Check). Faces and the loop consume Guard; sub-agents enforce a per-task
// cap through the same Guard, so there is one decision rule for the whole system.
package budget

import (
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// Producer attributes the budget events this package appends so a ceiling change
// is identifiable on the log without spending a frozen content kind on it.
const Producer = "/budget"

// DefaultWarnFraction is the fraction of the ceiling at which the guard starts
// warning when no threshold is configured (PRD §7.15: default 80%).
const DefaultWarnFraction = 0.8

// State is the enforcement decision a spend total maps to under a Guard. It is
// ordered OK < Warn < Halt so callers can compare severity directly.
type State int

const (
	// OK means spend is below the warning threshold (or no budget is set).
	OK State = iota
	// Warn means spend has crossed the warning threshold but not the ceiling: the
	// face shows a banner / status-line change, the turn continues.
	Warn
	// Halt means spend has reached or passed the ceiling: the loop finishes the
	// in-flight tool call, then stops before starting another priced turn.
	Halt
)

// String renders the state for logs and tests.
func (s State) String() string {
	switch s {
	case Warn:
		return "warn"
	case Halt:
		return "halt"
	default:
		return "ok"
	}
}

// Guard is the pure enforcement rule: a dollar ceiling plus the fraction of it
// at which warnings begin. The zero Guard (LimitUSD <= 0) is disabled — every
// spend is OK — so a budget-free session and a sub-agent with no cap both fall
// through cleanly. Guard holds no state; the spend total is supplied per check,
// so the same Guard enforces a session budget and a per-task sub-agent cap.
type Guard struct {
	// LimitUSD is the hard ceiling. A non-positive value disables the guard.
	LimitUSD float64
	// WarnFraction is the fraction of LimitUSD at which warnings begin; values
	// outside (0,1) fall back to DefaultWarnFraction.
	WarnFraction float64
}

// Enabled reports whether the guard has a ceiling to enforce.
func (g Guard) Enabled() bool { return g.LimitUSD > 0 }

// warnFraction returns the effective warning fraction, clamping an unset or
// nonsensical configured value to the default.
func (g Guard) warnFraction() float64 {
	if g.WarnFraction <= 0 || g.WarnFraction >= 1 {
		return DefaultWarnFraction
	}
	return g.WarnFraction
}

// WarnThresholdUSD is the dollar figure at which Check first returns Warn. It is
// zero for a disabled guard.
func (g Guard) WarnThresholdUSD() float64 {
	if !g.Enabled() {
		return 0
	}
	return g.LimitUSD * g.warnFraction()
}

// Check maps a session's total spend to an enforcement decision. A disabled
// guard always returns OK. The ceiling test is inclusive (spend == limit halts).
// Check is a pure decision over the spend it is given; whether the total can
// overshoot the ceiling depends on when the caller measures spend — the loop
// checks at turn boundaries, so a single turn's cost can still carry the total
// past the ceiling before the next check (see WithBudget).
func (g Guard) Check(spentUSD float64) State {
	if !g.Enabled() {
		return OK
	}
	switch {
	case spentUSD >= g.LimitUSD:
		return Halt
	case spentUSD >= g.WarnThresholdUSD():
		return Warn
	default:
		return OK
	}
}

// Set builds the budget event recording a new session ceiling of limitUSD, to be
// appended to the log. Setting a ceiling of 0 clears the budget (additive, not a
// deletion: the latest event wins). The returned block's Seq and timestamp are
// assigned at append time.
func Set(limitUSD float64) schema.Block {
	return eventlog.NewBudget(Producer, limitUSD)
}

// Current returns the session's active ceiling — the most recently set budget on
// the log — and whether one was ever set. A ceiling of 0 is a real value (budget
// cleared); callers treat <= 0 as "no enforcement" via Guard.Enabled.
func Current(events []schema.Block) (limitUSD float64, ok bool) {
	return eventlog.BudgetLimit(events)
}
