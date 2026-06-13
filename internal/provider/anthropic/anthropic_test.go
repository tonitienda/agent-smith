package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

func floatp(f float64) *float64 { return &f }

// --- request building -------------------------------------------------------

func TestBuildWireRequestExtractsSystemAndAlternates(t *testing.T) {
	req := provider.Request{
		Model: "claude-opus-4-8",
		Context: []schema.Block{
			{ID: "s1", Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: "be terse"}},
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}},
			{ID: "a1", Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: "hello"}},
			{ID: "a2", Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{
				ToolUseID: "toolu_1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`),
			}},
			{ID: "t1", Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{
				ToolUseID: "toolu_1", Content: []schema.Part{{Type: "text", Text: "file body"}},
			}},
		},
	}
	w, err := buildWireRequest(req, DefaultMaxTokens)
	if err != nil {
		t.Fatalf("buildWireRequest: %v", err)
	}

	if len(w.System) != 1 || w.System[0].Text != "be terse" {
		t.Fatalf("system = %+v, want one block 'be terse'", w.System)
	}
	// user(hi), assistant(hello + tool_use), user(tool_result) → 3 messages.
	if len(w.Messages) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(w.Messages), w.Messages)
	}
	if w.Messages[0].Role != "user" || w.Messages[1].Role != "assistant" || w.Messages[2].Role != "user" {
		t.Errorf("roles = %q/%q/%q, want user/assistant/user", w.Messages[0].Role, w.Messages[1].Role, w.Messages[2].Role)
	}
	// assistant message merged the text and the tool_use into one message.
	if len(w.Messages[1].Content) != 2 {
		t.Fatalf("assistant content = %+v, want 2 blocks (text+tool_use)", w.Messages[1].Content)
	}
	tu, ok := w.Messages[1].Content[1].(wireToolUse)
	if !ok || tu.ID != "toolu_1" || tu.Name != "read" {
		t.Errorf("assistant block 2 = %+v, want tool_use toolu_1/read", w.Messages[1].Content[1])
	}
	tr, ok := w.Messages[2].Content[0].(wireToolResult)
	if !ok || tr.ToolUseID != "toolu_1" {
		t.Errorf("user block = %+v, want tool_result for toolu_1", w.Messages[2].Content[0])
	}
}

func TestBuildWireRequestDefaultsAndParams(t *testing.T) {
	req := provider.Request{
		Model: "claude-opus-4-8",
		Params: provider.SamplingParams{
			Temperature:   floatp(0.5),
			TopP:          floatp(0.9),
			StopSequences: []string{"STOP"},
			Reasoning:     &provider.ReasoningOpts{BudgetTokens: 2048},
		},
		Context: []schema.Block{
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}},
		},
	}
	w, err := buildWireRequest(req, DefaultMaxTokens)
	if err != nil {
		t.Fatalf("buildWireRequest: %v", err)
	}
	if w.MaxTokens != DefaultMaxTokens {
		t.Errorf("max_tokens = %d, want default %d", w.MaxTokens, DefaultMaxTokens)
	}
	if w.Temperature == nil || *w.Temperature != 0.5 || w.TopP == nil || *w.TopP != 0.9 {
		t.Errorf("temperature/top_p = %v/%v, want 0.5/0.9", w.Temperature, w.TopP)
	}
	if len(w.StopSequences) != 1 || w.StopSequences[0] != "STOP" {
		t.Errorf("stop_sequences = %v, want [STOP]", w.StopSequences)
	}
	if w.Thinking == nil || w.Thinking.Type != "enabled" || w.Thinking.BudgetTokens != 2048 {
		t.Errorf("thinking = %+v, want enabled/2048", w.Thinking)
	}
	if !w.Stream {
		t.Error("stream = false, want true")
	}
}

func TestBuildWireRequestExplicitMaxTokens(t *testing.T) {
	req := provider.Request{
		Model:   "m",
		Params:  provider.SamplingParams{MaxTokens: 100},
		Context: []schema.Block{{ID: "u", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "x"}}},
	}
	w, _ := buildWireRequest(req, DefaultMaxTokens)
	if w.MaxTokens != 100 {
		t.Errorf("max_tokens = %d, want 100", w.MaxTokens)
	}
}

func TestBuildWireRequestCacheBreakpoints(t *testing.T) {
	req := provider.Request{
		Model: "m",
		Cache: provider.CacheHints{
			Mode:        schema.CacheModeExplicit,
			Breakpoints: []schema.CacheBreakpoint{{BlockID: "u1", TTL: "1h"}},
		},
		Context: []schema.Block{
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "cached"}},
		},
	}
	w, err := buildWireRequest(req, DefaultMaxTokens)
	if err != nil {
		t.Fatalf("buildWireRequest: %v", err)
	}
	tx, ok := w.Messages[0].Content[0].(wireText)
	if !ok || tx.CacheControl == nil || tx.CacheControl.Type != "ephemeral" || tx.CacheControl.TTL != "1h" {
		t.Errorf("content = %+v, want cache_control ephemeral/1h", w.Messages[0].Content[0])
	}
}

func TestBuildWireRequestReasoningRoundTrip(t *testing.T) {
	req := provider.Request{
		Model: "m",
		Context: []schema.Block{
			{ID: "r1", Kind: schema.KindReasoning, Role: schema.RoleAssistant, Reasoning: &schema.ReasoningBody{Text: "think", Signature: "sig"}},
			{ID: "r2", Kind: schema.KindReasoning, Role: schema.RoleAssistant, Reasoning: &schema.ReasoningBody{Encrypted: "ENC", Redacted: true}},
		},
	}
	w, err := buildWireRequest(req, DefaultMaxTokens)
	if err != nil {
		t.Fatalf("buildWireRequest: %v", err)
	}
	if len(w.Messages) != 1 || len(w.Messages[0].Content) != 2 {
		t.Fatalf("messages = %+v, want one assistant message with 2 blocks", w.Messages)
	}
	th, ok := w.Messages[0].Content[0].(wireThinkingBlock)
	if !ok || th.Thinking != "think" || th.Signature != "sig" {
		t.Errorf("block 1 = %+v, want thinking think/sig", w.Messages[0].Content[0])
	}
	rt, ok := w.Messages[0].Content[1].(wireRedactedThinking)
	if !ok || rt.Data != "ENC" {
		t.Errorf("block 2 = %+v, want redacted_thinking ENC", w.Messages[0].Content[1])
	}
}

func TestBuildWireRequestInvalidToolArguments(t *testing.T) {
	req := provider.Request{
		Model: "m",
		Context: []schema.Block{
			{ID: "a1", Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{
				ToolUseID: "toolu_1", Name: "read", Arguments: json.RawMessage(`{bad`),
			}},
		},
	}
	if _, err := buildWireRequest(req, DefaultMaxTokens); err == nil {
		t.Fatal("buildWireRequest accepted invalid tool arguments, want error")
	}
}

func TestBuildToolsDefaultsSchema(t *testing.T) {
	w, err := buildWireRequest(provider.Request{
		Model: "m",
		Tools: []provider.ToolDef{{Name: "ping", Description: "p"}},
	}, DefaultMaxTokens)
	if err != nil {
		t.Fatalf("buildWireRequest: %v", err)
	}
	if len(w.Tools) != 1 || w.Tools[0].Name != "ping" {
		t.Fatalf("tools = %+v, want one tool 'ping'", w.Tools)
	}
	if !json.Valid(w.Tools[0].InputSchema) || !strings.Contains(string(w.Tools[0].InputSchema), "object") {
		t.Errorf("input_schema = %s, want default object schema", w.Tools[0].InputSchema)
	}
}

// --- streaming end-to-end ---------------------------------------------------

// assembled reduces a normalized stream back into the turn it describes — the
// same job the loop (AS-018) does — to assert the events suffice to reconstruct
// a turn from the Anthropic wire format.
type assembled struct {
	model       string
	responseID  string
	stopReason  string
	usage       schema.Tokens
	serviceTier string
	blocks      []schema.Block
}

func assemble(t *testing.T, s provider.Stream) assembled {
	t.Helper()
	var a assembled
	open := map[int]*schema.Block{}
	rawArgs := map[int]string{}
	for s.Next() {
		ev := s.Event()
		switch ev.Type {
		case provider.EventTurnStart:
			if ev.Turn != nil {
				a.model = ev.Turn.Model
				a.responseID = ev.Turn.ResponseID
			}
		case provider.EventBlockStart:
			b := &schema.Block{Kind: ev.Header.Kind, Role: ev.Header.Role}
			switch ev.Header.Kind {
			case schema.KindText:
				b.Text = &schema.TextBody{}
			case schema.KindReasoning:
				b.Reasoning = &schema.ReasoningBody{}
			case schema.KindToolCall:
				b.ToolCall = &schema.ToolCallBody{ToolUseID: ev.Header.ToolUseID, Name: ev.Header.ToolName, ToolKind: ev.Header.ToolKind}
			}
			open[ev.BlockIndex] = b
		case provider.EventTextDelta:
			open[ev.BlockIndex].Text.Text += ev.TextDelta
		case provider.EventReasoningDelta:
			open[ev.BlockIndex].Reasoning.Text += ev.TextDelta
			open[ev.BlockIndex].Reasoning.Signature += ev.SignatureDelta
			open[ev.BlockIndex].Reasoning.Encrypted += ev.EncryptedDelta
		case provider.EventToolCallDelta:
			rawArgs[ev.BlockIndex] += ev.ArgumentsDelta
		case provider.EventBlockStop:
			b := open[ev.BlockIndex]
			if b.Kind == schema.KindToolCall {
				b.ToolCall.ArgumentsRaw = rawArgs[ev.BlockIndex]
				b.ToolCall.Arguments = json.RawMessage(rawArgs[ev.BlockIndex])
			}
			a.blocks = append(a.blocks, *b)
			delete(open, ev.BlockIndex)
		case provider.EventUsage:
			if ev.Usage != nil {
				if ev.Usage.Input != nil {
					a.usage.Input = ev.Usage.Input
				}
				if ev.Usage.Output != nil {
					a.usage.Output = ev.Usage.Output
				}
				if ev.Usage.CacheRead != nil {
					a.usage.CacheRead = ev.Usage.CacheRead
				}
				if ev.Usage.CacheWrite != nil {
					a.usage.CacheWrite = ev.Usage.CacheWrite
				}
			}
			if ev.UsageMeta != nil && ev.UsageMeta.ServiceTier != "" {
				a.serviceTier = ev.UsageMeta.ServiceTier
			}
		case provider.EventTurnStop:
			a.stopReason = ev.StopReason
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream ended with error: %v", err)
	}
	return a
}

// sseServer returns a provider pointed at a test server that replays the given
// SSE body and records the decoded request it received.
func sseServer(t *testing.T, sseBody string, gotReq *wireRequest) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing auth/version headers: %v", r.Header)
		}
		if gotReq != nil {
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, gotReq); err != nil {
				t.Errorf("decoding request body: %v", err)
			}
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseBody)
	}))
	t.Cleanup(srv.Close)
	return New("test-key", WithBaseURL(srv.URL))
}

func TestStreamTextTurn(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-8","usage":{"input_tokens":42,"cache_read_input_tokens":10,"output_tokens":1,"service_tier":"standard"}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: ping`,
		`data: {"type":"ping"}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	p := sseServer(t, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "claude-opus-4-8"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if got.model != "claude-opus-4-8" || got.responseID != "msg_1" {
		t.Errorf("turn info = %q/%q, want claude-opus-4-8/msg_1", got.model, got.responseID)
	}
	if len(got.blocks) != 1 || got.blocks[0].Text.Text != "Hello world" {
		t.Fatalf("blocks = %+v, want one text 'Hello world'", got.blocks)
	}
	if got.usage.Input == nil || *got.usage.Input != 42 || got.usage.Output == nil || *got.usage.Output != 7 {
		t.Errorf("usage = %+v, want input 42 output 7", got.usage)
	}
	if got.usage.CacheRead == nil || *got.usage.CacheRead != 10 {
		t.Errorf("cache_read = %v, want 10", got.usage.CacheRead)
	}
	if got.serviceTier != "standard" {
		t.Errorf("service_tier = %q, want standard", got.serviceTier)
	}
	if got.stopReason != provider.StopEndTurn {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopEndTurn)
	}
}

func TestStreamToolCallTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_2","model":"m","usage":{"input_tokens":5}}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_9","name":"grep","input":{}}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`,
		``,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	p := sseServer(t, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 1 || got.blocks[0].Kind != schema.KindToolCall {
		t.Fatalf("blocks = %+v, want one tool_call", got.blocks)
	}
	tc := got.blocks[0].ToolCall
	if tc.ToolUseID != "toolu_9" || tc.Name != "grep" || tc.ToolKind != schema.ToolKindClient {
		t.Errorf("tool call = %+v, want toolu_9/grep/client", tc)
	}
	if tc.ArgumentsRaw != `{"q":"x"}` {
		t.Errorf("arguments = %q, want {\"q\":\"x\"}", tc.ArgumentsRaw)
	}
	if got.stopReason != provider.StopToolUse {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopToolUse)
	}
}

func TestStreamReasoningTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"m","model":"m","usage":{"input_tokens":1}}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step "}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"one"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG"}}`,
		``,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"ENC"}}`,
		``,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	p := sseServer(t, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 2 {
		t.Fatalf("blocks = %+v, want 2 reasoning blocks", got.blocks)
	}
	if got.blocks[0].Reasoning.Text != "step one" || got.blocks[0].Reasoning.Signature != "SIG" {
		t.Errorf("reasoning[0] = %+v, want 'step one'/SIG", got.blocks[0].Reasoning)
	}
	if got.blocks[1].Reasoning.Encrypted != "ENC" {
		t.Errorf("reasoning[1].Encrypted = %q, want ENC", got.blocks[1].Reasoning.Encrypted)
	}
}

func TestStreamMissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	p := New("")
	if _, err := p.Stream(context.Background(), provider.Request{Model: "m"}); provider.KindOf(err) != provider.ErrAuth {
		t.Errorf("err kind = %v, want auth", provider.KindOf(err))
	}
}

func TestStreamMissingModel(t *testing.T) {
	p := New("k")
	if _, err := p.Stream(context.Background(), provider.Request{}); provider.KindOf(err) != provider.ErrInvalidRequest {
		t.Errorf("err kind = %v, want invalid_request", provider.KindOf(err))
	}
}

func TestNameIsAnthropic(t *testing.T) {
	if got := New("k").Name(); got != "anthropic" {
		t.Errorf("Name() = %q, want anthropic", got)
	}
}

func TestNewReadsEnvKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	if New("").apiKey != "env-key" {
		t.Error("New(\"\") did not fall back to ANTHROPIC_API_KEY")
	}
	if New("explicit").apiKey != "explicit" {
		t.Error("explicit key should win over env")
	}
}
