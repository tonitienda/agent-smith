package budget_test

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/schema"
)

func TestGuardCheck(t *testing.T) {
	// $0.50 ceiling, default 80% warn → warn at $0.40, halt at $0.50.
	g := budget.Guard{LimitUSD: 0.50}
	cases := []struct {
		spent float64
		want  budget.State
	}{
		{0, budget.OK},
		{0.39, budget.OK},
		{0.40, budget.Warn},
		{0.499, budget.Warn},
		{0.50, budget.Halt},
		{0.75, budget.Halt},
	}
	for _, c := range cases {
		if got := g.Check(c.spent); got != c.want {
			t.Errorf("Check(%.3f) = %v, want %v", c.spent, got, c.want)
		}
	}
}

func TestGuardDisabled(t *testing.T) {
	for _, g := range []budget.Guard{{}, {LimitUSD: 0}, {LimitUSD: -1}} {
		if g.Enabled() {
			t.Errorf("Guard(%v).Enabled() = true, want false", g)
		}
		if got := g.Check(1000); got != budget.OK {
			t.Errorf("disabled Check = %v, want OK", got)
		}
		if got := g.WarnThresholdUSD(); got != 0 {
			t.Errorf("disabled WarnThresholdUSD = %v, want 0", got)
		}
	}
}

func TestGuardWarnFractionFallback(t *testing.T) {
	// Out-of-range fractions fall back to the default; a valid one is honored.
	for _, frac := range []float64{0, -0.1, 1, 2} {
		g := budget.Guard{LimitUSD: 1, WarnFraction: frac}
		if got := g.WarnThresholdUSD(); got != budget.DefaultWarnFraction {
			t.Errorf("WarnFraction %v: threshold = %v, want default %v", frac, got, budget.DefaultWarnFraction)
		}
	}
	g := budget.Guard{LimitUSD: 1, WarnFraction: 0.5}
	if got := g.WarnThresholdUSD(); got != 0.5 {
		t.Errorf("threshold = %v, want 0.5", got)
	}
}

func TestSetCurrentRoundTrip(t *testing.T) {
	var events []schema.Block

	if _, ok := budget.Current(events); ok {
		t.Fatal("Current on empty log reported a budget")
	}

	events = append(events, budget.Set(0.50))
	limit, ok := budget.Current(events)
	if !ok || limit != 0.50 {
		t.Fatalf("Current = (%v, %v), want (0.50, true)", limit, ok)
	}

	// The latest budget event wins — a raised ceiling supersedes the earlier one.
	events = append(events, budget.Set(2))
	if limit, ok := budget.Current(events); !ok || limit != 2 {
		t.Fatalf("after raise Current = (%v, %v), want (2, true)", limit, ok)
	}

	// Clearing records a real 0 ceiling that disables enforcement.
	events = append(events, budget.Set(0))
	limit, ok = budget.Current(events)
	if !ok || limit != 0 {
		t.Fatalf("after clear Current = (%v, %v), want (0, true)", limit, ok)
	}
	if (budget.Guard{LimitUSD: limit}).Enabled() {
		t.Error("cleared budget still enabled")
	}
}
