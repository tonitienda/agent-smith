package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// --- Chat Completions request building --------------------------------------

func TestBuildChatRequestMergesAssistantAndMaps(t *testing.T) {
	req := provider.Request{
		Model: "grok-4.3",
		Context: []schema.Block{
			{ID: "s1", Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: "be terse"}},
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}},
			{ID: "a1", Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: "calling"}},
			{ID: "a2", Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{
				ToolUseID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`),
			}},
			{ID: "t1", Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{
				ToolUseID: "call_1", Content: []schema.Part{{Type: "text", Text: "body"}},
			}},
		},
	}
	w, err := buildChatRequest(req)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	// system, user, assistant(text+tool_calls), tool = 4 messages.
	if len(w.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(w.Messages), w.Messages)
	}
	if w.Messages[0].Role != "system" || w.Messages[1].Role != "user" || w.Messages[2].Role != "assistant" || w.Messages[3].Role != "tool" {
		t.Errorf("roles = %q/%q/%q/%q", w.Messages[0].Role, w.Messages[1].Role, w.Messages[2].Role, w.Messages[3].Role)
	}
	asst := w.Messages[2]
	if asst.Content != "calling" {
		t.Errorf("assistant content = %v, want 'calling'", asst.Content)
	}
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "call_1" || asst.ToolCalls[0].Function.Name != "read" {
		t.Errorf("assistant tool_calls = %+v, want one call_1/read", asst.ToolCalls)
	}
	if w.Messages[3].ToolCallID != "call_1" || w.Messages[3].Content != "body" {
		t.Errorf("tool message = %+v, want call_1/'body'", w.Messages[3])
	}
}

func TestBuildChatRequestParamsAndStreamOptions(t *testing.T) {
	req := provider.Request{
		Model: "gpt-4o",
		Params: provider.SamplingParams{
			MaxTokens:     256,
			Temperature:   floatp(0.2),
			StopSequences: []string{"STOP"},
		},
		Context: []schema.Block{{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}}},
	}
	w, err := buildChatRequest(req)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	if w.MaxTokens != 256 {
		t.Errorf("max_tokens = %d, want 256", w.MaxTokens)
	}
	if len(w.Stop) != 1 || w.Stop[0] != "STOP" {
		t.Errorf("stop = %v, want [STOP]", w.Stop)
	}
	if w.StreamOptions == nil || !w.StreamOptions.IncludeUsage {
		t.Errorf("stream_options = %+v, want include_usage true", w.StreamOptions)
	}
}

func TestBuildChatRequestMultimodalParts(t *testing.T) {
	req := provider.Request{
		Model: "gpt-4o",
		Context: []schema.Block{
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{
				Parts: []schema.Part{
					{Type: "text", Text: "look:"},
					{Type: "image", MediaType: "image/png", Data: "QQ=="},
				},
			}},
		},
	}
	w, err := buildChatRequest(req)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	parts, ok := w.Messages[0].Content.([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %+v, want 2 parts", w.Messages[0].Content)
	}
	img, ok := parts[1].(chatImagePart)
	if !ok || img.ImageURL.URL != "data:image/png;base64,QQ==" {
		t.Errorf("image part = %+v, want data URI", parts[1])
	}
}

// --- Chat Completions streaming end-to-end ----------------------------------

func TestChatStreamTextTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		``,
		`data: {"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		``,
		`data: {"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"}}]}`,
		``,
		`data: {"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: {"id":"chatcmpl_1","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":4}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceChatCompletions, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if got.model != "gpt-4o" || got.responseID != "chatcmpl_1" {
		t.Errorf("turn info = %q/%q, want gpt-4o/chatcmpl_1", got.model, got.responseID)
	}
	if len(got.blocks) != 1 || got.blocks[0].Text.Text != "Hello world" {
		t.Fatalf("blocks = %+v, want one text 'Hello world'", got.blocks)
	}
	if got.usage.Input == nil || *got.usage.Input != 11 || got.usage.Output == nil || *got.usage.Output != 2 {
		t.Errorf("usage = %+v, want input 11 output 2", got.usage)
	}
	if got.usage.CacheRead == nil || *got.usage.CacheRead != 4 {
		t.Errorf("cache_read = %v, want 4", got.usage.CacheRead)
	}
	if got.stopReason != provider.StopEndTurn {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopEndTurn)
	}
}

func TestChatStreamToolCallTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"c","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"grep","arguments":""}}]}}]}`,
		``,
		`data: {"id":"c","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		``,
		`data: {"id":"c","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		``,
		`data: {"id":"c","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceChatCompletions, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 1 || got.blocks[0].Kind != schema.KindToolCall {
		t.Fatalf("blocks = %+v, want one tool_call", got.blocks)
	}
	tc := got.blocks[0].ToolCall
	if tc.ToolUseID != "call_9" || tc.Name != "grep" || tc.ToolKind != schema.ToolKindClient {
		t.Errorf("tool call = %+v, want call_9/grep/client", tc)
	}
	if tc.ArgumentsRaw != `{"q":"x"}` {
		t.Errorf("arguments = %q, want {\"q\":\"x\"}", tc.ArgumentsRaw)
	}
	if got.stopReason != provider.StopToolUse {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopToolUse)
	}
}

// TestChatStreamGrokReasoning checks the Grok reasoning_content extension maps
// to a reasoning block (AC: preserve compatible-endpoint extensions) without
// affecting plain text behavior.
func TestChatStreamGrokReasoning(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"c","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"think "}}]}`,
		``,
		`data: {"id":"c","model":"grok-4.3","choices":[{"index":0,"delta":{"reasoning_content":"hard"}}]}`,
		``,
		`data: {"id":"c","model":"grok-4.3","choices":[{"index":0,"delta":{"content":"answer"}}]}`,
		``,
		`data: {"id":"c","model":"grok-4.3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceChatCompletions, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "grok-4.3"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 2 {
		t.Fatalf("blocks = %+v, want reasoning + text", got.blocks)
	}
	if got.blocks[0].Kind != schema.KindReasoning || got.blocks[0].Reasoning.Text != "think hard" {
		t.Errorf("block[0] = %+v, want reasoning 'think hard'", got.blocks[0])
	}
	if got.blocks[1].Kind != schema.KindText || got.blocks[1].Text.Text != "answer" {
		t.Errorf("block[1] = %+v, want text 'answer'", got.blocks[1])
	}
}

// TestChatStreamCompatibleEndpointNoUsage simulates a minimal OpenAI-compatible
// endpoint (e.g. a local server) that omits the usage chunk and ends with EOF
// rather than a [DONE] sentinel — the basic chat turn must still complete (AC:
// missing optional fields never crash; pointing base_url at a compatible
// endpoint completes a basic chat turn).
func TestChatStreamCompatibleEndpointNoUsage(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"local-1","model":"llama","choices":[{"index":0,"delta":{"content":"hi there"}}]}`,
		``,
		`data: {"id":"local-1","model":"llama","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceChatCompletions, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "llama"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)
	if len(got.blocks) != 1 || got.blocks[0].Text.Text != "hi there" {
		t.Fatalf("blocks = %+v, want one text 'hi there'", got.blocks)
	}
	if got.stopReason != provider.StopEndTurn {
		t.Errorf("stop = %q, want %q (deferred stop flushed on EOF)", got.stopReason, provider.StopEndTurn)
	}
	if got.usage.Input != nil || got.usage.Output != nil {
		t.Errorf("usage = %+v, want empty (endpoint reported none)", got.usage)
	}
}

func TestChatRequestUsesChatEndpoint(t *testing.T) {
	var raw []byte
	body := strings.Join([]string{
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	p := sseServer(t, SurfaceChatCompletions, body, &raw)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !strings.Contains(string(raw), `"stream_options"`) {
		t.Errorf("body = %s, want stream_options", raw)
	}
}
