package compact_test

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/compact"
	"github.com/tonitienda/agent-smith/internal/config"
)

func TestConfigFromDefaultsWhenAbsent(t *testing.T) {
	cfg, warns := compact.ConfigFrom(config.New())
	if cfg.Auto {
		t.Error("auto-compaction on by default; want off")
	}
	if cfg.AutoThreshold != compact.DefaultAutoThreshold {
		t.Errorf("threshold %g, want default %g", cfg.AutoThreshold, compact.DefaultAutoThreshold)
	}
	if len(warns) != 0 {
		t.Errorf("absent section warned: %v", warns)
	}
}

func TestConfigFromReadsValues(t *testing.T) {
	cfg, warns := compact.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"compact": map[string]any{"auto": true, "auto_threshold": 0.7},
	})))
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if !cfg.Auto || cfg.AutoThreshold != 0.7 {
		t.Errorf("got %+v, want {Auto:true AutoThreshold:0.7}", cfg)
	}
}

func TestConfigFromHigherLayerOverrides(t *testing.T) {
	cfg, _ := compact.ConfigFrom(config.New(
		config.MapLayer("user", "user.json", map[string]any{
			"compact": map[string]any{"auto_threshold": 0.5},
		}),
		config.MapLayer("project", "config.json", map[string]any{
			"compact": map[string]any{"auto_threshold": 0.6},
		}),
	))
	if cfg.AutoThreshold != 0.6 {
		t.Errorf("project override lost: got %g, want 0.6", cfg.AutoThreshold)
	}
}

func TestConfigFromOutOfRangeThresholdFallsBackWithWarning(t *testing.T) {
	// An explicit but out-of-range threshold defaults *and* warns (D2).
	for _, bad := range []float64{0, 1, -0.2, 1.4} {
		cfg, warns := compact.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
			"compact": map[string]any{"auto": true, "auto_threshold": bad},
		})))
		if cfg.AutoThreshold != compact.DefaultAutoThreshold {
			t.Errorf("threshold %g: got %g, want default %g", bad, cfg.AutoThreshold, compact.DefaultAutoThreshold)
		}
		if len(warns) != 1 {
			t.Errorf("threshold %g: want 1 warning, got %v", bad, warns)
		}
	}
}

func TestConfigFromBadTypeDegradesWithWarning(t *testing.T) {
	cfg, warns := compact.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"compact": map[string]any{"auto": "yes"},
	})))
	if cfg.Auto || cfg.AutoThreshold != compact.DefaultAutoThreshold {
		t.Errorf("malformed section: got %+v, want defaults", cfg)
	}
	if len(warns) == 0 {
		t.Error("malformed section did not warn")
	}
}
