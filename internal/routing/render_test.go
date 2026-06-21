package routing

import (
	"strings"
	"testing"
)

func TestRenderShowsTiersFeaturesAndRecentCalls(t *testing.T) {
	p := Default()
	p.set(Standard, "anthropic", "claude-sonnet-4-6")
	p.setFeature("compact", Standard)

	out := Render(p, []Call{
		{Index: 1, Model: "claude-haiku-4-5"}, // cheap tier
		{Index: 2, Model: "claude-opus-4-8"},  // unmapped -> main
	})

	for _, want := range []string{
		"cheap", "claude-haiku-4-5", "gpt-4o-mini",
		"standard", "claude-sonnet-4-6",
		"Feature overrides", "compact",
		"Recent calls", "#1", "#2", unmapped,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n---\n%s", want, out)
		}
	}
	// The cheap-tier turn is labeled with its tier, the opus turn as the main model.
	if !strings.Contains(out, "#1") || !strings.Contains(out, "cheap") {
		t.Error("recent cheap call not labeled cheap")
	}
}

func TestRenderEmptyTierAndNoOverrides(t *testing.T) {
	out := Render(Default(), nil)
	if !strings.Contains(out, "falls back to the active model") {
		t.Errorf("strong/standard tiers should note fallback\n%s", out)
	}
	if !strings.Contains(out, "(none") {
		t.Errorf("no feature overrides should say none\n%s", out)
	}
	if strings.Contains(out, "Recent calls") {
		t.Errorf("no recent calls should omit the section\n%s", out)
	}
}
