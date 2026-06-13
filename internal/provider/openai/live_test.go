package openai

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// TestLiveAgenticTurn is the AS-010 end-to-end smoke test against the real
// OpenAI API (Responses surface). It is skipped unless SMITH_LIVE_OPENAI=1 and
// an API key are set, so it never runs in CI. It drives a full agentic turn —
// user → assistant with a tool call → tool result → assistant — and checks the
// stream normalizes and usage is reported.
//
//	SMITH_LIVE_OPENAI=1 OPENAI_API_KEY=sk-... \
//	  go test ./internal/provider/openai -run TestLiveAgenticTurn -v
//
// To smoke-test an OpenAI-compatible endpoint over the Chat Completions surface
// instead, set SMITH_LIVE_OPENAI_SURFACE=chat_completions and point
// SMITH_LIVE_OPENAI_BASE_URL / SMITH_LIVE_OPENAI_MODEL at it.
func TestLiveAgenticTurn(t *testing.T) {
	if os.Getenv("SMITH_LIVE_OPENAI") != "1" {
		t.Skip("set SMITH_LIVE_OPENAI=1 (and OPENAI_API_KEY) to run the live smoke test")
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	model := os.Getenv("SMITH_LIVE_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	opts := []Option{}
	if base := os.Getenv("SMITH_LIVE_OPENAI_BASE_URL"); base != "" {
		opts = append(opts, WithBaseURL(base))
	}
	if surf := os.Getenv("SMITH_LIVE_OPENAI_SURFACE"); surf != "" {
		opts = append(opts, WithSurface(Surface(surf)))
	}
	p := New("", opts...)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	weather := provider.ToolDef{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`),
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

	if turn1.stopReason != provider.StopToolUse {
		t.Fatalf("turn 1 stop = %q, want tool_use (model did not call the tool)", turn1.stopReason)
	}
	var call *schema.Block
	for i := range turn1.blocks {
		if turn1.blocks[i].Kind == schema.KindToolCall {
			call = &turn1.blocks[i]
		}
	}
	if call == nil {
		t.Fatal("turn 1 produced no tool_call block")
	}

	// Turn 2: feed the tool result back; expect a natural-language answer.
	ctx2 := append([]schema.Block{}, req.Context...)
	ctx2 = append(ctx2, *call, schema.Block{
		ID: "t1", Kind: schema.KindToolResult, Role: schema.RoleTool,
		ToolResult: &schema.ToolResultBody{
			ToolUseID: call.ToolCall.ToolUseID,
			Content:   []schema.Part{{Type: "text", Text: `{"city":"Paris","temp_c":21,"summary":"sunny"}`}},
		},
	})
	s2, err := p.Stream(ctx, provider.Request{Model: model, Params: req.Params, Tools: req.Tools, Context: ctx2})
	if err != nil {
		t.Fatalf("turn 2 Stream: %v", err)
	}
	turn2 := assemble(t, s2)
	t.Logf("turn 2: stop=%s blocks=%d input=%v output=%v", turn2.stopReason, len(turn2.blocks), turn2.usage.Input, turn2.usage.Output)

	if turn2.usage.Input == nil || turn2.usage.Output == nil {
		t.Error("turn 2 usage not reported")
	}
	var sawText bool
	for i := range turn2.blocks {
		if turn2.blocks[i].Kind == schema.KindText && turn2.blocks[i].Text.Text != "" {
			sawText = true
		}
	}
	if !sawText {
		t.Error("turn 2 produced no assistant text answer")
	}
}
