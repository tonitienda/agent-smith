package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/factdetector"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// shellCall / shellResult build the failed-then-successful shell pattern the
// built-in rediscovered-fact detector (AS-048) keys on, so a scripted session can
// drive a real finding through the composition-root wiring.
func shellCall(id, command string) schema.Block {
	args, _ := json.Marshal(map[string]string{"command": command})
	return schema.Block{
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: id, Name: "shell", Arguments: args},
	}
}

func shellResult(id string, failed bool) schema.Block {
	exit := 0
	if failed {
		exit = 1
	}
	return schema.Block{
		Kind:       schema.KindToolResult,
		Role:       schema.RoleTool,
		ToolResult: &schema.ToolResultBody{ToolUseID: id, IsError: failed, ExitCode: &exit},
	}
}

// TestBuildSubAgentsRegistersBuiltin asserts the composition root registers the
// built-in sub-agent so a real session has it available (AS-107 AC1, the
// registration half).
func TestBuildSubAgentsRegistersBuiltin(t *testing.T) {
	reg, store, err := buildSubAgents(nil, nil, nil, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("buildSubAgents: %v", err)
	}
	if store == nil {
		t.Fatal("buildSubAgents returned a nil store")
	}
	m, ok := reg.Effective(factdetector.Name)
	if !ok {
		t.Fatalf("built-in %q not registered", factdetector.Name)
	}
	if !m.EnabledByDefault {
		t.Fatalf("built-in %q should default on", factdetector.Name)
	}
}

// TestBuildSubAgentsConfigOverlay drives the one-config-line property end-to-end
// (AS-107 AC2, §7.19): `subagents.<name>.enabled = false` disables the built-in
// through the composition root, and an entry for an unknown sub-agent warns rather
// than failing startup.
func TestBuildSubAgentsConfigOverlay(t *testing.T) {
	cfg := config.New(config.MapLayer("project", "test", map[string]any{
		"subagents": map[string]any{
			factdetector.Name: map[string]any{"enabled": false},
			"no-such-agent":   map[string]any{"enabled": true},
		},
	}))
	var stderr bytes.Buffer
	reg, _, err := buildSubAgents(cfg, nil, nil, nil, &stderr)
	if err != nil {
		t.Fatalf("buildSubAgents: %v", err)
	}
	m, ok := reg.Effective(factdetector.Name)
	if !ok {
		t.Fatalf("built-in %q not registered", factdetector.Name)
	}
	if m.EnabledByDefault {
		t.Fatal("config enabled=false did not disable the built-in")
	}
	if !strings.Contains(stderr.String(), "no-such-agent") {
		t.Fatalf("expected a warning about the unknown sub-agent, got %q", stderr.String())
	}
}

// TestBuildSubAgentsProducesFinding scripts a session that rediscovers a working
// command and asserts the wired registry + Runner record a finding into the store
// the composition root owns (AS-107 AC3) — the seam /insights (AS-045) will read.
func TestBuildSubAgentsProducesFinding(t *testing.T) {
	reg, store, err := buildSubAgents(nil, nil, nil, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("buildSubAgents: %v", err)
	}
	const session = "sess-1"
	runner := subagent.NewRunner(reg, store, session)

	// Drive the framework lifecycle as the loop does: open the session scope,
	// observe each block, then tear it down over the session's slice.
	slice := []schema.Block{
		shellCall("c1", "npm test"),
		shellResult("c1", true),
		shellCall("c2", "go test ./..."),
		shellResult("c2", false),
	}
	scope := subagent.Scope{Kind: subagent.SessionScope}
	runner.Begin(scope)
	for _, b := range slice {
		runner.Observe(b)
	}
	runner.End(scope, slice)

	findings := store.Findings(session)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding in the owned store, got %d: %+v", len(findings), findings)
	}
	if findings[0].Kind != factdetector.FindingKind {
		t.Fatalf("wrong finding kind %q", findings[0].Kind)
	}
	if findings[0].SubAgent != factdetector.Name {
		t.Fatalf("finding not attributed to %q: %q", factdetector.Name, findings[0].SubAgent)
	}
}
