package loop_test

import (
	"context"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

func intp(n int) *int { return &n }

// TestUsageRecordedAndAccumulated drives a turn that reports usage in two events
// (input/cache at the start, output at the end, as Anthropic does) and asserts
// the loop appends one usage event with the field-wise sum, the serving model,
// vendor, stop reason, and metadata — the AS-020 capture the cost engine prices.
func TestUsageRecordedAndAccumulated(t *testing.T) {
	events := []provider.Event{
		{Type: provider.EventTurnStart, Turn: &provider.TurnInfo{}},
		{Type: provider.EventUsage,
			Usage:     &schema.Tokens{Input: intp(100), CacheRead: intp(40), CacheWrite: intp(20)},
			UsageMeta: &schema.UsageMeta{ServiceTier: "standard"}},
		{Type: provider.EventBlockStart, Header: &provider.BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}},
		{Type: provider.EventTextDelta, TextDelta: "hello"},
		{Type: provider.EventBlockStop},
		{Type: provider.EventUsage, Usage: &schema.Tokens{Output: intp(60)}},
		{Type: provider.EventTurnStop, StopReason: provider.StopEndTurn},
	}
	p := &provider.Mock{NameValue: "mock", Events: events}
	h := newHarness(t, p, nil)

	if _, err := h.engine.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var usage *schema.Block
	count := 0
	for _, b := range h.log.Events() {
		if b.Kind == eventlog.KindUsage {
			count++
			bb := b
			usage = &bb
		}
	}
	if count != 1 {
		t.Fatalf("usage events on log = %d, want 1", count)
	}
	if usage.Tokens == nil {
		t.Fatal("usage block has no tokens")
	}
	if got := deref(usage.Tokens.Input); got != 100 {
		t.Errorf("input = %d, want 100", got)
	}
	if got := deref(usage.Tokens.Output); got != 60 {
		t.Errorf("output = %d, want 60", got)
	}
	if got := deref(usage.Tokens.CacheRead); got != 40 {
		t.Errorf("cache read = %d, want 40", got)
	}
	if got := deref(usage.Tokens.CacheWrite); got != 20 {
		t.Errorf("cache write = %d, want 20", got)
	}
	if usage.StopReason != provider.StopEndTurn {
		t.Errorf("stop reason = %q, want %q", usage.StopReason, provider.StopEndTurn)
	}
	if usage.Provider == nil || usage.Provider.Vendor != "mock" || usage.Provider.Model != "test-model" {
		t.Errorf("provider = %+v, want vendor mock model test-model", usage.Provider)
	}
	if usage.UsageMeta == nil || usage.UsageMeta.ServiceTier != "standard" {
		t.Errorf("usage meta = %+v, want service tier standard", usage.UsageMeta)
	}
	if usage.Role != schema.RoleHarness {
		t.Errorf("role = %q, want harness", usage.Role)
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// TestUsageNotProjected guards that usage records are control-only: they must
// never appear in the model-facing context.
func TestUsageNotProjected(t *testing.T) {
	events := []provider.Event{
		{Type: provider.EventTurnStart, Turn: &provider.TurnInfo{}},
		{Type: provider.EventBlockStart, Header: &provider.BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}},
		{Type: provider.EventTextDelta, TextDelta: "hello"},
		{Type: provider.EventBlockStop},
		{Type: provider.EventUsage, Usage: &schema.Tokens{Input: intp(10), Output: intp(5)}},
		{Type: provider.EventTurnStop, StopReason: provider.StopEndTurn},
	}
	p := &provider.Mock{Events: events}
	h := newHarness(t, p, nil)
	if _, err := h.engine.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	proj := projection.Project(h.log.Events(), projection.Options{})
	for _, b := range proj.Live() {
		if b.Kind == eventlog.KindUsage {
			t.Fatal("usage block leaked into model-facing projection")
		}
	}
}

// TestNoUsageNoRecord ensures a turn that reports no usage adds no usage event,
// so providers that omit usage never pollute the log with empty records.
func TestNoUsageNoRecord(t *testing.T) {
	p := &provider.Mock{Events: provider.TextTurn("hi", "")}
	h := newHarness(t, p, nil)
	if _, err := h.engine.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, b := range h.log.Events() {
		if b.Kind == eventlog.KindUsage {
			t.Fatal("a usage-free turn must not append a usage event")
		}
	}
}
