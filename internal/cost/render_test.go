package cost_test

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/schema"
)

func TestRenderEmpty(t *testing.T) {
	out := cost.Render(cost.Summarize(nil, cost.Embedded()))
	if !strings.Contains(out, "No usage recorded") {
		t.Errorf("empty render = %q", out)
	}
}

func TestRenderTableAndTotals(t *testing.T) {
	tbl := cost.Embedded()
	s := cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(12000), Output: ptr(340), CacheRead: ptr(5000)}),
	}, tbl)
	out := cost.Render(s)

	for _, want := range []string{
		"Session cost (USD)",
		"claude-opus-4-8",
		"12,000", // thousands separators
		"5,000",
		"Cache savings",
		"$", // a dollar figure rendered
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderUnknownModelNote(t *testing.T) {
	s := cost.Summarize([]schema.Block{
		usageBlock("mystery-9", &schema.Tokens{Input: ptr(10)}),
	}, cost.Embedded())
	out := cost.Render(s)
	if !strings.Contains(out, "no pricing entry") {
		t.Errorf("expected an unpriced-model note:\n%s", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("expected the unknown mark in the cost column:\n%s", out)
	}
}

func TestCommasFormatting(t *testing.T) {
	// Exercised indirectly, but pin the boundary cases via a small render.
	s := cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(1000000)}),
	}, cost.Embedded())
	if out := cost.Render(s); !strings.Contains(out, "1,000,000") {
		t.Errorf("million not grouped:\n%s", out)
	}
}
