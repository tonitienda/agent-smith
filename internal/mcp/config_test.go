package mcp

import (
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/config"
)

func TestParseServers(t *testing.T) {
	cfg := config.New(config.MapLayer("project", "test", map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"github": map[string]any{
					"command": "github-mcp-server",
					"args":    []any{"stdio"},
					"env":     map[string]any{"TOKEN": "x"},
					"timeout": "10s",
				},
				"remote": map[string]any{
					"url":     "https://example.test/mcp",
					"headers": map[string]any{"Authorization": "Bearer y"},
				},
				"broken-both": map[string]any{"command": "x", "url": "https://y"},
				"broken-none": map[string]any{},
				"bad-timeout": map[string]any{"command": "z", "timeout": "soon"},
			},
		},
	}))

	specs, warns, err := ConfigFrom(cfg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Sorted by name; the two broken-config entries are dropped with warnings, the
	// bad-timeout entry is kept (default timeout) with a warning.
	if len(specs) != 3 {
		t.Fatalf("specs = %d (%+v), want 3", len(specs), specs)
	}
	byName := map[string]ServerConfig{}
	for _, s := range specs {
		byName[s.Name] = s
	}
	if g := byName["github"]; g.Command != "github-mcp-server" || len(g.Args) != 1 || g.Timeout != 10*time.Second {
		t.Fatalf("github spec = %+v", g)
	}
	if r := byName["remote"]; r.URL == "" || r.Headers["Authorization"] != "Bearer y" {
		t.Fatalf("remote spec = %+v", r)
	}
	if len(warns) != 3 {
		t.Fatalf("warnings = %v, want 3 (both, none, bad-timeout)", warns)
	}
}

func TestParseAbsent(t *testing.T) {
	specs, warns, err := ConfigFrom(config.New())
	if err != nil || specs != nil || warns != nil {
		t.Fatalf("empty config: specs=%v warns=%v err=%v", specs, warns, err)
	}
}
