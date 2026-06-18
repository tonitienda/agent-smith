package openai

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

// assembled reduces a normalized stream back into the turn it describes — the
// same job the loop (AS-018) does — to assert the events suffice to reconstruct
// a turn from either OpenAI surface.
type assembled struct {
	model      string
	responseID string
	stopReason string
	usage      schema.Tokens
	blocks     []schema.Block
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
				b.ToolCall = &schema.ToolCallBody{
					ToolUseID: ev.Header.ToolUseID, Name: ev.Header.ToolName,
					ToolKind: ev.Header.ToolKind, ToolSubtype: ev.Header.ToolSubtype,
				}
			}
			open[ev.BlockIndex] = b
		case provider.EventTextDelta:
			open[ev.BlockIndex].Text.Text += ev.TextDelta
		case provider.EventReasoningDelta:
			open[ev.BlockIndex].Reasoning.Text += ev.TextDelta
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
			mergeUsage(&a.usage, ev.Usage)
		case provider.EventTurnStop:
			a.stopReason = ev.StopReason
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream ended with error: %v", err)
	}
	return a
}

func mergeUsage(dst *schema.Tokens, u *schema.Tokens) {
	if u == nil {
		return
	}
	if u.Input != nil {
		dst.Input = u.Input
	}
	if u.Output != nil {
		dst.Output = u.Output
	}
	if u.CacheRead != nil {
		dst.CacheRead = u.CacheRead
	}
	if u.Reasoning != nil {
		dst.Reasoning = u.Reasoning
	}
}

// sseServer returns a provider for the given surface, pointed at a test server
// that replays the given SSE body and records the decoded raw request body.
func sseServer(t *testing.T, surface Surface, sseBody string, gotBody *[]byte) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") == "" {
			t.Errorf("missing authorization header: %v", r.Header)
		}
		if gotBody != nil {
			body, _ := io.ReadAll(r.Body)
			*gotBody = body
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseBody)
	}))
	t.Cleanup(srv.Close)
	return New("test-key", WithBaseURL(srv.URL), WithSurface(surface))
}

// --- Responses request building --------------------------------------------

func TestBuildResponsesRequestSystemAndItems(t *testing.T) {
	req := provider.Request{
		Model: "gpt-5",
		Context: []schema.Block{
			{ID: "s1", Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: "be terse"}},
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}},
			{ID: "a1", Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: "hello"}},
			{ID: "a2", Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{
				ToolUseID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`),
			}},
			{ID: "t1", Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{
				ToolUseID: "call_1", Content: []schema.Part{{Type: "text", Text: "file body"}},
			}},
		},
	}
	w, err := buildResponsesRequest(req)
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if w.Instructions != "be terse" {
		t.Errorf("instructions = %q, want 'be terse'", w.Instructions)
	}
	// user message, assistant message, function_call, function_call_output = 4 items.
	if len(w.Input) != 4 {
		t.Fatalf("input = %d items, want 4: %+v", len(w.Input), w.Input)
	}
	fc, ok := w.Input[2].(responsesFunctionCall)
	if !ok || fc.CallID != "call_1" || fc.Name != "read" || fc.Arguments != `{"path":"x"}` {
		t.Errorf("input[2] = %+v, want function_call call_1/read", w.Input[2])
	}
	fco, ok := w.Input[3].(responsesFunctionCallOutput)
	if !ok || fco.CallID != "call_1" || fco.Output != "file body" {
		t.Errorf("input[3] = %+v, want function_call_output call_1/'file body'", w.Input[3])
	}
}

// TestBuildResponsesRequestRendersCompaction confirms a derived compaction
// block (AS-038) reaches the model as an input item rather than being dropped.
func TestBuildResponsesRequestRendersCompaction(t *testing.T) {
	req := provider.Request{
		Model: "gpt-5",
		Context: []schema.Block{
			{ID: "c1", Kind: schema.KindCompaction, Role: schema.RoleUser, Text: &schema.TextBody{Text: "summary of earlier"}},
			{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "and now this"}},
		},
	}
	w, err := buildResponsesRequest(req)
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if len(w.Input) != 2 {
		t.Fatalf("input = %d items, want 2 (compaction + text)", len(w.Input))
	}
	raw, _ := json.Marshal(w.Input)
	if !strings.Contains(string(raw), "summary of earlier") {
		t.Errorf("input does not carry the summary: %s", raw)
	}
}

func TestBuildResponsesRequestParams(t *testing.T) {
	req := provider.Request{
		Model: "gpt-5",
		Params: provider.SamplingParams{
			MaxTokens:   1000,
			Temperature: floatp(0.5),
			TopP:        floatp(0.9),
			Reasoning:   &provider.ReasoningOpts{Effort: "high"},
		},
		Context: []schema.Block{{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}}},
	}
	w, err := buildResponsesRequest(req)
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if w.MaxOutputTokens != 1000 {
		t.Errorf("max_output_tokens = %d, want 1000", w.MaxOutputTokens)
	}
	if w.Temperature == nil || *w.Temperature != 0.5 || w.TopP == nil || *w.TopP != 0.9 {
		t.Errorf("temperature/top_p = %v/%v, want 0.5/0.9", w.Temperature, w.TopP)
	}
	if w.Reasoning == nil || w.Reasoning.Effort != "high" {
		t.Errorf("reasoning = %+v, want effort high", w.Reasoning)
	}
	if w.Store {
		t.Error("store = true, want false (stateless)")
	}
	if !w.Stream {
		t.Error("stream = false, want true")
	}
}

func TestBuildResponsesReasoningReuse(t *testing.T) {
	req := provider.Request{
		Model: "gpt-5",
		Context: []schema.Block{
			// Visible-only reasoning is dropped; encrypted reasoning is re-sent.
			{ID: "r1", Kind: schema.KindReasoning, Role: schema.RoleAssistant, Reasoning: &schema.ReasoningBody{Text: "visible"}},
			{ID: "r2", Kind: schema.KindReasoning, Role: schema.RoleAssistant,
				Provider:  &schema.Provider{NativeID: "rs_1"},
				Reasoning: &schema.ReasoningBody{Encrypted: "ENC", Summary: []string{"sum"}}},
		},
	}
	w, err := buildResponsesRequest(req)
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if len(w.Input) != 1 {
		t.Fatalf("input = %d items, want 1 (encrypted reasoning only): %+v", len(w.Input), w.Input)
	}
	ri, ok := w.Input[0].(responsesReasoningItem)
	if !ok || ri.EncryptedContent != "ENC" || ri.ID != "rs_1" {
		t.Errorf("input[0] = %+v, want reasoning ENC/rs_1", w.Input[0])
	}
}

func TestBuildResponsesInvalidToolArguments(t *testing.T) {
	req := provider.Request{
		Model: "gpt-5",
		Context: []schema.Block{
			{ID: "a1", Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{
				ToolUseID: "call_1", Name: "read", ArgumentsRaw: `{bad`,
			}},
		},
	}
	if _, err := buildResponsesRequest(req); err == nil {
		t.Fatal("buildResponsesRequest accepted invalid tool arguments, want error")
	}
}

func TestBuildResponsesOrphanToolResultDegradesToMessage(t *testing.T) {
	req := provider.Request{
		Model: "gpt-5",
		Context: []schema.Block{
			{ID: "t1", Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{
				ToolUseID: "call_missing", Content: []schema.Part{{Type: "text", Text: "file body"}},
			}},
		},
	}
	w, err := buildResponsesRequest(req)
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if len(w.Input) != 1 {
		t.Fatalf("input = %d items, want 1", len(w.Input))
	}
	msg, ok := w.Input[0].(responsesMessage)
	if !ok {
		t.Fatalf("input[0] = %#v, want responsesMessage", w.Input[0])
	}
	if msg.Role != "user" || len(msg.Content) != 1 {
		t.Fatalf("message = %#v, want one user content part", msg)
	}
	part, ok := msg.Content[0].(responsesInputText)
	if !ok {
		t.Fatalf("content[0] = %#v, want responsesInputText", msg.Content[0])
	}
	if part.Text != "Tool result:\nfile body" {
		t.Fatalf("text = %q, want degraded tool result text", part.Text)
	}
}

// --- Responses streaming end-to-end ----------------------------------------

func TestResponsesStreamTextTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"delta":"Hello"}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"delta":" world"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":42,"output_tokens":7,"input_tokens_details":{"cached_tokens":10},"output_tokens_details":{"reasoning_tokens":3}}}}`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if got.model != "gpt-5" || got.responseID != "resp_1" {
		t.Errorf("turn info = %q/%q, want gpt-5/resp_1", got.model, got.responseID)
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
	if got.usage.Reasoning == nil || *got.usage.Reasoning != 3 {
		t.Errorf("reasoning tokens = %v, want 3", got.usage.Reasoning)
	}
	if got.stopReason != provider.StopEndTurn {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopEndTurn)
	}
}

func TestResponsesStreamToolCallTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_2","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_9","name":"grep"}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"q\":"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"x\"}"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_2","status":"completed","usage":{"input_tokens":5,"output_tokens":12}}}`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
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

func TestResponsesStreamReasoningTurn(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"step "}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"one"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","encrypted_content":"ENC"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":3}}}`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 1 || got.blocks[0].Kind != schema.KindReasoning {
		t.Fatalf("blocks = %+v, want one reasoning block", got.blocks)
	}
	if got.blocks[0].Reasoning.Text != "step one" || got.blocks[0].Reasoning.Encrypted != "ENC" {
		t.Errorf("reasoning = %+v, want 'step one'/ENC", got.blocks[0].Reasoning)
	}
}

func TestResponsesStreamMaxTokensIncomplete(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"delta":"trunc"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`,
		``,
		`data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":4}}}`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)
	if got.stopReason != provider.StopMaxTokens {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopMaxTokens)
	}
}

func TestResponsesStreamServerToolCall(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"web_search_call","id":"ws_1"}}`,
		``,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"r","status":"completed"}}`,
		``,
	}, "\n")

	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)
	if len(got.blocks) != 1 {
		t.Fatalf("blocks = %+v, want one server tool_call", got.blocks)
	}
	tc := got.blocks[0].ToolCall
	if tc.ToolKind != schema.ToolKindServer || tc.Name != "web_search" || tc.ToolSubtype != "web_search_call" {
		t.Errorf("server tool = %+v, want server/web_search/web_search_call", tc)
	}
	if got.stopReason != provider.StopToolUse {
		t.Errorf("stop = %q, want %q", got.stopReason, provider.StopToolUse)
	}
}

func TestResponsesRequestEndpointAndBody(t *testing.T) {
	var raw []byte
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"r","status":"completed"}}`,
		``,
	}, "\n")
	// Use a server that records the request path too.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	p := New("k", WithBaseURL(srv.URL))
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_, _ = provider.Collect(s)
	if gotPath != responsesPath {
		t.Errorf("path = %q, want %q", gotPath, responsesPath)
	}
	if !strings.Contains(string(raw), `"stream":true`) {
		t.Errorf("body = %s, want stream:true", raw)
	}
}

// --- construction -----------------------------------------------------------

func TestStreamMissingKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
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

func TestNameIsOpenAI(t *testing.T) {
	if got := New("k").Name(); got != "openai" {
		t.Errorf("Name() = %q, want openai", got)
	}
}

func TestNewReadsEnvKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	if New("").apiKey != "env-key" {
		t.Error("New(\"\") did not fall back to OPENAI_API_KEY")
	}
	if New("explicit").apiKey != "explicit" {
		t.Error("explicit key should win over env")
	}
}

func TestDefaultSurfaceIsResponses(t *testing.T) {
	if New("k").surface != SurfaceResponses {
		t.Errorf("default surface = %q, want responses", New("k").surface)
	}
}

func TestWithBaseURLTrimsTrailingSlash(t *testing.T) {
	if got := New("k", WithBaseURL("https://x/")).baseURL; got != "https://x" {
		t.Errorf("baseURL = %q, want https://x", got)
	}
}
