package manifest

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

func intp(n int) *int { return &n }

// usageBlock is a minimal priced-turn record for the manifest's totals/models.
func usageBlock(model string, in, out int) schema.Block {
	return schema.Block{
		ID:       schema.NewID(),
		Kind:     eventlog.KindUsage,
		Role:     schema.RoleAssistant,
		Provider: &schema.Provider{Model: model},
		Tokens:   &schema.Tokens{Input: intp(in), Output: intp(out)},
	}
}

func toolCallBlock(name string) schema.Block {
	return schema.Block{
		ID:       schema.NewID(),
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: schema.NewID(), Name: name},
	}
}

func TestBuildDerivesModelsToolsAndTotals(t *testing.T) {
	events := []schema.Block{
		{ID: schema.NewID(), Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}},
		toolCallBlock("read"),
		toolCallBlock("read"), // duplicate collapses
		toolCallBlock("shell"),
		usageBlock("model-a", 100, 20),
		usageBlock("model-b", 50, 10),
	}
	m := Build(Input{
		SessionID: "sess_x",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		Events:    events,
		Cost:      cost.Summarize(events, cost.Embedded()),
	})

	if got, want := strings.Join(m.Models, ","), "model-a,model-b"; got != want {
		t.Errorf("models = %q, want %q", got, want)
	}
	if got, want := strings.Join(m.Tools, ","), "read,shell"; got != want {
		t.Errorf("tools = %q, want %q", got, want)
	}
	if m.Turns != 2 {
		t.Errorf("turns = %d, want 2", m.Turns)
	}
	if m.EventCount != len(events) {
		t.Errorf("event_count = %d, want %d", m.EventCount, len(events))
	}
	if m.Totals.InputTokens != 150 || m.Totals.OutputTokens != 30 {
		t.Errorf("token totals = in %d out %d, want in 150 out 30", m.Totals.InputTokens, m.Totals.OutputTokens)
	}
	if m.Totals.TotalTokens != 180 {
		t.Errorf("total tokens = %d, want 180", m.Totals.TotalTokens)
	}
}

// TestSanitizeDropsSecretsCanary is the AS-055 canary: a secret-bearing config key
// never reaches the manifest, while non-secret keys survive.
func TestSanitizeDropsSecretsCanary(t *testing.T) {
	const canary = "sk-CANARY-do-not-leak"
	in := map[string]any{
		"model":                   "model-a",
		"provider.api_key":        canary,
		"mcp.servers.x.token":     canary,
		"auth.credential":         canary,
		"telemetry.otel_endpoint": "http://localhost:4318",
	}
	out := Sanitize(in)

	for k := range out {
		if isSecretKey(k) {
			t.Errorf("secret-looking key %q survived sanitization", k)
		}
	}
	if _, ok := out["model"]; !ok {
		t.Error("non-secret key model was dropped")
	}
	if _, ok := out["telemetry.otel_endpoint"]; !ok {
		t.Error("non-secret telemetry endpoint was dropped")
	}
	// The canary must not appear anywhere in the rendered manifest config.
	m := Build(Input{SessionID: "s", Config: in, Cost: cost.Summarize(nil, cost.Embedded())})
	for k, v := range m.Config {
		if s, ok := v.(string); ok && strings.Contains(s, canary) {
			t.Fatalf("canary secret leaked into manifest config under %q", k)
		}
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := Build(Input{
		SessionID:     "sess_y",
		BinaryVersion: "smith test",
		CreatedAt:     time.Unix(1700000000, 0).UTC(),
		Events:        []schema.Block{usageBlock("model-a", 10, 2)},
		Cost:          cost.Summarize([]schema.Block{usageBlock("model-a", 10, 2)}, cost.Embedded()),
	})
	if err := Write(dir, m); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok, err := Read(dir)
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if got.SessionID != "sess_y" || got.SchemaVersion != schemaVersion {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestReadMissingManifest(t *testing.T) {
	_, ok, err := Read(t.TempDir())
	if err != nil {
		t.Fatalf("read missing: unexpected err %v", err)
	}
	if ok {
		t.Error("expected ok=false for a directory with no manifest")
	}
}
