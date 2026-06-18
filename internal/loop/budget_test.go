package loop_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// loopingTool provider: every turn requests the same tool, so the loop would run
// to maxIterations unless a guard stops it — the setup for budget enforcement.
func loopingProvider() *provider.Mock {
	return &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		return provider.ToolCallTurn("toolu_x", "echo", json.RawMessage(`{"msg":"x"}`)), nil
	}}
}

// TestBudgetWarnThenHalt is the AS-041 acceptance check: a $0.50 budget warns at
// $0.40 and halts before exceeding $0.50, using a spend that grows $0.10 per
// completed turn. The loop stops with StopBudget, having emitted one warning and
// one halt — well short of the 100-iteration safety valve.
func TestBudgetWarnThenHalt(t *testing.T) {
	var spent float64
	h := newHarness(t, loopingProvider(), []tool.Tool{echoTool()},
		loop.WithMaxIterations(100),
		loop.WithBudget(func() float64 { return spent }, 0.50, 0.8))
	// Each completed turn costs $0.10. The guard reads spend at the next turn
	// boundary, so spend reaches $0.40 (warn) then $0.50 (halt).
	h.onEvent = func(ev loop.UIEvent) {
		if ev.Kind == loop.UITurnComplete {
			spent += 0.10
		}
	}

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != loop.StopBudget {
		t.Fatalf("StopReason = %q, want %q", res.StopReason, loop.StopBudget)
	}
	if got := h.kinds(loop.UIBudgetWarning); got != 1 {
		t.Errorf("budget warnings = %d, want 1", got)
	}
	if got := h.kinds(loop.UIBudgetHalt); got != 1 {
		t.Errorf("budget halts = %d, want 1", got)
	}
	if res.Iterations >= 100 {
		t.Errorf("Iterations = %d, expected budget to halt well before the safety valve", res.Iterations)
	}
}

// TestBudgetOverrideFromLog verifies the ceiling set on the log (a /budget
// override) takes precedence over the configured default and survives into the
// run — the same path a resumed session replays. A $0.20 override on a log with a
// $1.00 default halts after the first $0.10 turn pushes spend to $0.20.
func TestBudgetOverrideFromLog(t *testing.T) {
	var spent float64
	h := newHarness(t, loopingProvider(), []tool.Tool{echoTool()},
		loop.WithMaxIterations(100),
		loop.WithBudget(func() float64 { return spent }, 1.00, 0.8))
	if _, err := h.log.Append(budget.Set(0.20)); err != nil {
		t.Fatalf("append budget: %v", err)
	}
	h.onEvent = func(ev loop.UIEvent) {
		if ev.Kind == loop.UITurnComplete {
			spent += 0.10
		}
	}

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != loop.StopBudget {
		t.Fatalf("StopReason = %q, want %q", res.StopReason, loop.StopBudget)
	}
	// Halt at the $0.20 override, not the $1.00 default: two turns ($0.20) then stop.
	if res.Iterations > 3 {
		t.Errorf("Iterations = %d, want the $0.20 override to halt early", res.Iterations)
	}
}

// TestBudgetReservationHaltsBeforeOvershoot is the AS-086 acceptance check for
// the pre-turn estimate: with $0 spent so far but a worst-case next-turn cost of
// $0.60 reserved against a $0.50 ceiling, the loop halts *before* issuing the
// turn — spend never overshoots. The boundary check alone (TestBudgetWarnThenHalt)
// would have let the turn run first.
func TestBudgetReservationHaltsBeforeOvershoot(t *testing.T) {
	h := newHarness(t, loopingProvider(), []tool.Tool{echoTool()},
		loop.WithMaxIterations(100),
		loop.WithBudget(func() float64 { return 0 }, 0.50, 0.8),
		loop.WithBudgetReservation(
			func([]schema.Block) (float64, bool) { return 0.60, true }, false))

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != loop.StopBudget {
		t.Fatalf("StopReason = %q, want %q", res.StopReason, loop.StopBudget)
	}
	// Halted before any turn ran: no provider turn completed.
	if res.Iterations != 0 {
		t.Errorf("Iterations = %d, want 0 (halt before issuing the turn)", res.Iterations)
	}
	if got := h.kinds(loop.UIBudgetHalt); got != 1 {
		t.Errorf("budget halts = %d, want 1", got)
	}
}

// TestBudgetReservationFitsProceeds confirms a worst-case reservation that fits
// under the ceiling does not block the turn: the reservation guard only halts an
// overshoot, otherwise the run proceeds normally.
func TestBudgetReservationFitsProceeds(t *testing.T) {
	calls := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		calls++
		if calls == 1 {
			return provider.ToolCallTurn("toolu_1", "echo", json.RawMessage(`{"msg":"hi"}`)), nil
		}
		return provider.TextTurn("done", ""), nil
	}}
	h := newHarness(t, p, []tool.Tool{echoTool()},
		loop.WithBudget(func() float64 { return 0 }, 0.50, 0.8),
		loop.WithBudgetReservation(
			func([]schema.Block) (float64, bool) { return 0.01, true }, false))

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != provider.StopEndTurn {
		t.Errorf("StopReason = %q, want end_turn (reservation fits)", res.StopReason)
	}
	if h.kinds(loop.UIBudgetHalt) != 0 {
		t.Error("budget halted though the reservation fit under the ceiling")
	}
}

// TestBudgetUnpricedWarnsOnce verifies the unpriced-model path: a budget is set
// but the model cannot be priced (reserve reports ok=false). With halt disabled
// the loop surfaces the notice exactly once and keeps running, rather than
// silently spending against a ceiling it cannot enforce.
func TestBudgetUnpricedWarnsOnce(t *testing.T) {
	calls := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		calls++
		if calls <= 2 {
			return provider.ToolCallTurn("toolu_x", "echo", json.RawMessage(`{"msg":"x"}`)), nil
		}
		return provider.TextTurn("done", ""), nil
	}}
	h := newHarness(t, p, []tool.Tool{echoTool()},
		loop.WithMaxIterations(100),
		loop.WithBudget(func() float64 { return 0 }, 0.50, 0.8),
		loop.WithBudgetReservation(
			func([]schema.Block) (float64, bool) { return 0, false }, false))

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != provider.StopEndTurn {
		t.Errorf("StopReason = %q, want end_turn (warn-only unpriced)", res.StopReason)
	}
	if got := h.kinds(loop.UIBudgetUnpriced); got != 1 {
		t.Errorf("unpriced notices = %d, want exactly 1", got)
	}
	if h.kinds(loop.UIBudgetHalt) != 0 {
		t.Error("budget halted though halt-on-unpriced was disabled")
	}
}

// TestBudgetUnpricedHaltsWhenConfigured verifies the config-flagged halt: with
// halt-on-unpriced enabled, a budgeted session on an unpriced model stops before
// the first turn rather than spending blind.
func TestBudgetUnpricedHaltsWhenConfigured(t *testing.T) {
	h := newHarness(t, loopingProvider(), []tool.Tool{echoTool()},
		loop.WithMaxIterations(100),
		loop.WithBudget(func() float64 { return 0 }, 0.50, 0.8),
		loop.WithBudgetReservation(
			func([]schema.Block) (float64, bool) { return 0, false }, true))

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != loop.StopBudget {
		t.Fatalf("StopReason = %q, want %q", res.StopReason, loop.StopBudget)
	}
	if res.Iterations != 0 {
		t.Errorf("Iterations = %d, want 0 (halt before spending on an unpriced model)", res.Iterations)
	}
	if got := h.kinds(loop.UIBudgetUnpriced); got != 1 {
		t.Errorf("unpriced notices = %d, want 1", got)
	}
}

// TestBudgetReservationFallsBackToBoundary confirms AS-041's boundary path still
// governs when no reservation is installed: behavior degrades gracefully to the
// existing warn-then-halt check.
func TestBudgetReservationFallsBackToBoundary(t *testing.T) {
	var spent float64
	h := newHarness(t, loopingProvider(), []tool.Tool{echoTool()},
		loop.WithMaxIterations(100),
		loop.WithBudget(func() float64 { return spent }, 0.50, 0.8)) // no reservation
	h.onEvent = func(ev loop.UIEvent) {
		if ev.Kind == loop.UITurnComplete {
			spent += 0.10
		}
	}

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != loop.StopBudget {
		t.Fatalf("StopReason = %q, want %q", res.StopReason, loop.StopBudget)
	}
	// The turn runs before the boundary catches it, so spend reaches the ceiling.
	if res.Iterations == 0 {
		t.Error("Iterations = 0, want the boundary path to run turns before halting")
	}
}

// TestBudgetDisabledByDefault confirms a session with no budget configured and no
// override runs unaffected — enforcement is opt-in.
func TestBudgetDisabledByDefault(t *testing.T) {
	calls := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		calls++
		if calls == 1 {
			return provider.ToolCallTurn("toolu_1", "echo", json.RawMessage(`{"msg":"hi"}`)), nil
		}
		return provider.TextTurn("done", ""), nil
	}}
	h := newHarness(t, p, []tool.Tool{echoTool()},
		loop.WithBudget(func() float64 { return 1000 }, 0, 0.8)) // no ceiling

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != provider.StopEndTurn {
		t.Errorf("StopReason = %q, want end_turn (budget disabled)", res.StopReason)
	}
	if h.kinds(loop.UIBudgetHalt) != 0 {
		t.Error("budget halted with no ceiling set")
	}
}
