package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// toolCallWant is the canonical expectation for a get_weather(city=Paris) call:
// arguments are preserved verbatim, byte-for-byte.
var toolCallWant = &Want{
	StopReason: provider.StopToolUse,
	Blocks: []BlockExpect{
		{
			Kind: schema.KindToolCall, Role: schema.RoleAssistant,
			ToolName: "get_weather", ToolKind: schema.ToolKindClient,
			ArgumentsRaw: `{"city":"Paris"}`, RequireToolUseID: true,
		},
	},
}

// TestSuiteCatchesArgumentReformatting is the AS-012 regression test: it proves
// the suite would fail an adapter that reformats tool-call arguments instead of
// preserving them verbatim — the classic cross-provider normalization bug the
// conformance acceptance criteria call out. A correct adapter streams the
// arguments byte-for-byte; a buggy one re-serializes them (here, re-marshalling
// through a map adds a space and may reorder keys). Compare must catch it.
func TestSuiteCatchesArgumentReformatting(t *testing.T) {
	// A correct turn: arguments arrive exactly as the model emitted them.
	good := mockTurn(`{"city":"Paris"}`)
	got, err := assembleTurn(t, good)
	if err != nil {
		t.Fatalf("assembling good turn: %v", err)
	}
	if diffs := Compare(toolCallWant, got); len(diffs) != 0 {
		t.Fatalf("correct adapter flagged as divergent: %v", diffs)
	}

	// A buggy turn: the adapter re-serialized the arguments through a Go map,
	// which reformats the JSON (added whitespace) rather than preserving it.
	var m map[string]any
	if err := json.Unmarshal([]byte(`{"city":"Paris"}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reformatted, _ := json.MarshalIndent(m, "", "  ")
	bad := mockTurn(string(reformatted))
	got, err = assembleTurn(t, bad)
	if err != nil {
		t.Fatalf("assembling bad turn: %v", err)
	}
	diffs := Compare(toolCallWant, got)
	if len(diffs) == 0 {
		t.Fatal("suite did not catch reformatted tool-call arguments")
	}
	if !containsSubstr(diffs, "arguments") {
		t.Errorf("expected an arguments mismatch, got: %v", diffs)
	}
}

// TestAssembleDetectsUnterminatedBlock guards the assembler itself: a stream that
// opens a block and never closes it is a malformed normalization the suite must
// not silently accept.
func TestAssembleDetectsUnterminatedBlock(t *testing.T) {
	m := &provider.Mock{Events: []provider.Event{
		{Type: provider.EventTurnStart, Turn: &provider.TurnInfo{ResponseID: "x", Model: "m"}},
		{Type: provider.EventBlockStart, Header: &provider.BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}},
		{Type: provider.EventTextDelta, TextDelta: "oops"},
		// no block_stop, no turn_stop
	}}
	s, err := m.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := Assemble(s); err == nil {
		t.Fatal("Assemble accepted a stream with an unterminated block, want error")
	}
}

// mockTurn scripts a single tool-call turn whose arguments stream as argsRaw.
func mockTurn(argsRaw string) *provider.Mock {
	return &provider.Mock{Events: []provider.Event{
		{Type: provider.EventTurnStart, Turn: &provider.TurnInfo{ResponseID: "resp_x", Model: "mock-1"}},
		{Type: provider.EventBlockStart, Header: &provider.BlockHeader{
			Kind: schema.KindToolCall, Role: schema.RoleAssistant,
			ToolUseID: "call_1", ToolName: "get_weather", ToolKind: schema.ToolKindClient,
		}},
		{Type: provider.EventToolCallDelta, ArgumentsDelta: argsRaw},
		{Type: provider.EventBlockStop},
		{Type: provider.EventTurnStop, StopReason: provider.StopToolUse},
	}}
}

func assembleTurn(t *testing.T, m *provider.Mock) (Result, error) {
	t.Helper()
	s, err := m.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return Assemble(s)
}

func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
