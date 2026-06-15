package cost_test

import (
	"os"
	"path/filepath"
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

// TestRenderCacheSavingsLowerBound checks the savings dollar figure is marked a
// lower bound when the session has an unpriced turn (its cache reads count in
// tokens but contribute no dollars).
func TestRenderCacheSavingsLowerBound(t *testing.T) {
	s := cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{CacheRead: ptr(5000)}),
		usageBlock("mystery-9", &schema.Tokens{CacheRead: ptr(9999)}),
	}, cost.Embedded())
	out := cost.Render(s)
	if !strings.Contains(out, "lower bound") {
		t.Errorf("unpriced session should mark cache savings a lower bound:\n%s", out)
	}
	// Token count still reflects every cached read, priced or not.
	if !strings.Contains(out, "14,999") {
		t.Errorf("cache-read token total should include unpriced turns:\n%s", out)
	}
}

// TestRenderNonUSDCurrency checks amounts use the currency code (not a bare "$")
// when an override sets a non-USD currency, so the header and figures agree.
func TestRenderNonUSDCurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eur.json")
	eur := `{"version":1,"currency":"EUR","models":[
		{"model":"claude-opus-4-*","input_per_mtok":10.0,"output_per_mtok":20.0}
	]}`
	if err := os.WriteFile(path, []byte(eur), 0o600); err != nil {
		t.Fatal(err)
	}
	tbl, err := cost.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	out := cost.Render(cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(1000), Output: ptr(500)}),
	}, tbl))

	if !strings.Contains(out, "Session cost (EUR)") {
		t.Errorf("header should name EUR:\n%s", out)
	}
	if !strings.Contains(out, "EUR ") {
		t.Errorf("amounts should be prefixed with the EUR code:\n%s", out)
	}
	if strings.Contains(out, "$") {
		t.Errorf("non-USD render must not use a $ symbol:\n%s", out)
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
