package redaction_test

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/redaction"
)

func TestConfigFromDefaultsOff(t *testing.T) {
	cfg, warns := redaction.ConfigFrom(config.New())
	if cfg.Enabled {
		t.Error("redaction on by default; want off")
	}
	if len(warns) != 0 {
		t.Errorf("absent section warned: %v", warns)
	}
}

func TestConfigFromReadsValues(t *testing.T) {
	cfg, warns := redaction.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"redaction": map[string]any{"enabled": true, "extra_patterns": []any{"COMPANY-[0-9]+"}},
	})))
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if !cfg.Enabled {
		t.Error("enabled should be true")
	}
	if len(cfg.ExtraPatterns) != 1 || cfg.ExtraPatterns[0] != "COMPANY-[0-9]+" {
		t.Errorf("extra patterns: %v", cfg.ExtraPatterns)
	}
}
