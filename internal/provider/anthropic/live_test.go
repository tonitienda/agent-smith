package anthropic

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// TestLiveAgenticTurn is the AS-009 end-to-end smoke test against the real
// Anthropic API. It is skipped unless SMITH_LIVE_ANTHROPIC=1 and an API key are
// set, so it never runs in CI. It drives a full agentic turn — user → assistant
// with a tool call → tool result → assistant — and checks the stream normalizes
// and usage is reported.
//
//	SMITH_LIVE_ANTHROPIC=1 ANTHROPIC_API_KEY=sk-... \
//	  go test ./internal/provider/anthropic -run TestLiveAgenticTurn -v
func TestLiveAgenticTurn(t *testing.T) {
	if os.Getenv("SMITH_LIVE_ANTHROPIC") != "1" {
		t.Skip("set SMITH_LIVE_ANTHROPIC=1 (and ANTHROPIC_API_KEY) to run the live smoke test")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	model := os.Getenv("SMITH_LIVE_ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	p := New("")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	weather := provider.ToolDef{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}

	// Turn 1: prompt that should elicit a tool call.
	req := provider.Request{
		Model:  model,
		Params: provider.SamplingParams{MaxTokens: 1024},
		Tools:  []provider.ToolDef{weather},
		Context: []schema.Block{
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{
				Text: "Use the get_weather tool to check the weather in Paris, then tell me.",
			}},
		},
	}
	s, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("turn 1 Stream: %v", err)
	}
	turn1 := assemble(t, s)
	t.Logf("turn 1: stop=%s blocks=%d input=%v output=%v", turn1.stopReason, len(turn1.blocks), turn1.usage.Input, turn1.usage.Output)

	if turn1.usage.Input == nil || turn1.usage.Output == nil {
		t.Error("turn 1 did not report input and output usage")
	}

	var call *schema.Block
	for i := range turn1.blocks {
		if turn1.blocks[i].Kind == schema.KindToolCall {
			call = &turn1.blocks[i]
		}
	}
	if turn1.stopReason != provider.StopToolUse || call == nil {
		t.Fatalf("turn 1 did not call a tool (stop=%s); cannot complete the round trip", turn1.stopReason)
	}

	// Turn 2: feed the assistant blocks back plus a tool_result.
	ctx2 := append([]schema.Block{}, req.Context...)
	for i := range turn1.blocks {
		b := turn1.blocks[i]
		b.ID = "a" + string(b.Kind) // any stable unique-ish id is fine for a smoke test
		ctx2 = append(ctx2, b)
	}
	ctx2 = append(ctx2, schema.Block{
		ID: "tr1", Kind: schema.KindToolResult, Role: schema.RoleTool,
		ToolResult: &schema.ToolResultBody{
			ToolUseID: call.ToolCall.ToolUseID,
			Content:   []schema.Part{{Type: "text", Text: "18°C and sunny"}},
		},
	})

	s2, err := p.Stream(ctx, provider.Request{Model: model, Params: provider.SamplingParams{MaxTokens: 1024}, Tools: []provider.ToolDef{weather}, Context: ctx2})
	if err != nil {
		t.Fatalf("turn 2 Stream: %v", err)
	}
	turn2 := assemble(t, s2)
	t.Logf("turn 2: stop=%s blocks=%d", turn2.stopReason, len(turn2.blocks))
	if turn2.stopReason != provider.StopEndTurn {
		t.Errorf("turn 2 stop = %q, want end_turn", turn2.stopReason)
	}
}
