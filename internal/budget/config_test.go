package budget_test

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/config"
)

// configFrom is the feature-level behavior under test: read the `budget` section
// of layered config into a validated Config. The tests drive it through real
// *config.Config layers (not a hand-rolled fake) so the merge/provenance the
// view relies on is exercised end to end (Classical strategy).

func TestConfigFromDefaultsWhenAbsent(t *testing.T) {
	cfg, warns := budget.ConfigFrom(config.New())
	if cfg != (budget.Config{}) {
		t.Errorf("absent budget section: got %+v, want zero Config", cfg)
	}
	if len(warns) != 0 {
		t.Errorf("absent section warned: %v", warns)
	}
}

func TestConfigFromReadsValues(t *testing.T) {
	cfg, warns := budget.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"budget": map[string]any{
			"limit_usd":     5.0,
			"warn_fraction": 0.8,
			"halt_unpriced": true,
		},
	})))
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	want := budget.Config{DefaultLimitUSD: 5, WarnFraction: 0.8, HaltUnpriced: true}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestConfigFromHigherLayerOverrides(t *testing.T) {
	// A user-layer default ceiling overridden by the project layer: the typed view
	// must see the winning (merged) leaf, exercising config's provenance.
	cfg, _ := budget.ConfigFrom(config.New(
		config.MapLayer("user", "user.json", map[string]any{
			"budget": map[string]any{"limit_usd": 1.0},
		}),
		config.MapLayer("project", "config.json", map[string]any{
			"budget": map[string]any{"limit_usd": 9.0},
		}),
	))
	if cfg.DefaultLimitUSD != 9 {
		t.Errorf("project override lost: got limit %g, want 9", cfg.DefaultLimitUSD)
	}
}

func TestConfigFromBadTypeDegradesWithWarning(t *testing.T) {
	cfg, warns := budget.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"budget": map[string]any{"limit_usd": "lots"},
	})))
	if cfg != (budget.Config{}) {
		t.Errorf("malformed section: got %+v, want zero Config", cfg)
	}
	if len(warns) == 0 {
		t.Error("malformed section did not warn")
	}
}

func TestConfigFromValidatesRanges(t *testing.T) {
	cfg, warns := budget.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"budget": map[string]any{"limit_usd": -2.0, "warn_fraction": 1.5},
	})))
	if cfg.DefaultLimitUSD != 0 || cfg.WarnFraction != 0 {
		t.Errorf("out-of-range values kept: %+v", cfg)
	}
	if len(warns) != 2 {
		t.Errorf("want 2 warnings (negative limit, bad fraction), got %v", warns)
	}
}
