package permission_test

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/permission"
)

func TestConfigFromAbsentSectionIsZero(t *testing.T) {
	cfg, err := permission.ConfigFrom(config.New())
	if err != nil {
		t.Fatalf("ConfigFrom: %v", err)
	}
	if cfg.DefaultMode != "" || len(cfg.Tools) != 0 || len(cfg.Allow) != 0 {
		t.Errorf("absent section: got %+v, want zero Config", cfg)
	}
}

func TestConfigFromReadsSection(t *testing.T) {
	cfg, err := permission.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"permissions": map[string]any{
			"default_mode": "allowlist",
			"tools":        map[string]any{"read": "auto"},
			"allow":        []any{map[string]any{"tool": "shell", "pattern": "git status*"}},
		},
	})))
	if err != nil {
		t.Fatalf("ConfigFrom: %v", err)
	}
	if cfg.DefaultMode != permission.ModeAllowlist {
		t.Errorf("default_mode: got %q, want allowlist", cfg.DefaultMode)
	}
	if cfg.Tools["read"] != permission.ModeAuto {
		t.Errorf("tools[read]: got %q, want auto", cfg.Tools["read"])
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0] != (permission.Rule{Tool: "shell", Pattern: "git status*"}) {
		t.Errorf("allow: got %+v", cfg.Allow)
	}
}

func TestConfigFromHigherLayerOverrides(t *testing.T) {
	cfg, err := permission.ConfigFrom(config.New(
		config.MapLayer("user", "user.json", map[string]any{
			"permissions": map[string]any{"default_mode": "ask"},
		}),
		config.MapLayer("project", "config.json", map[string]any{
			"permissions": map[string]any{"default_mode": "auto"},
		}),
	))
	if err != nil {
		t.Fatalf("ConfigFrom: %v", err)
	}
	if cfg.DefaultMode != permission.ModeAuto {
		t.Errorf("project override lost: got %q, want auto", cfg.DefaultMode)
	}
}

func TestConfigFromMalformedSectionFailsClosed(t *testing.T) {
	// permissions is a safety boundary: a malformed section is a hard error, not a
	// silent downgrade to "ask".
	_, err := permission.ConfigFrom(config.New(config.MapLayer("project", "config.json", map[string]any{
		"permissions": map[string]any{"allow": "not-a-list"},
	})))
	if err == nil {
		t.Error("malformed permissions section did not error")
	}
}
