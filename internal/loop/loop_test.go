package loop_test

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// harness wires an Engine over an in-memory log, a registry of the given tools,
// and the supplied provider, recording every UIEvent the run emits.
type harness struct {
	engine  *loop.Engine
	log     *eventlog.Log
	events  []loop.UIEvent
	onEvent func(loop.UIEvent) // optional per-test hook, run after recording
}

func newHarness(t *testing.T, p provider.Provider, tools []tool.Tool, opts ...loop.Option) *harness {
	t.Helper()
	lg := eventlog.New()
	reg := tool.NewRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}
	rt := tool.NewRuntime(reg, lg)
	h := &harness{log: lg}
	opts = append(opts, loop.WithObserver(func(ev loop.UIEvent) {
		h.events = append(h.events, ev)
		if h.onEvent != nil {
			h.onEvent(ev)
		}
	}))
	e, err := loop.New(p, lg, rt, reg, "test-model", opts...)
	if err != nil {
		t.Fatalf("loop.New: %v", err)
	}
	h.engine = e
	return h
}

func (h *harness) kinds(k loop.UIEventKind) int {
	n := 0
	for _, ev := range h.events {
		if ev.Kind == k {
			n++
		}
	}
	return n
}

// echoTool returns its "msg" argument prefixed, so a test can assert the result
// flowed back through the loop.
func echoTool() tool.Tool {
	return tool.Func{
		Spec: tool.Def{
			Name:        "echo",
			Description: "echo a message",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
		},
		Fn: func(_ context.Context, args json.RawMessage) (tool.Output, error) {
			var in struct {
				Msg string `json:"msg"`
			}
			_ = json.Unmarshal(args, &in)
			return tool.Output{Text: "echo:" + in.Msg}, nil
		},
	}
}

// resultText concatenates the text parts of a tool_result body.
func resultText(b *schema.ToolResultBody) string {
	var sb strings.Builder
	for _, p := range b.Content {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// TestRunMultiTurnMultiTool drives a full session against the mock: the model
// calls a tool, the loop dispatches it and feeds the result back, and a second
// turn produces the final text. It is the AS-018 end-to-end acceptance check.
func TestRunMultiTurnMultiTool(t *testing.T) {
	calls := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		calls++
		if calls == 1 {
			return provider.ToolCallTurn("toolu_1", "echo", json.RawMessage(`{"msg":"hi"}`)), nil
		}
		return provider.TextTurn("all done", ""), nil
	}}
	h := newHarness(t, p, []tool.Tool{echoTool()})

	res, err := h.engine.Run(context.Background(), "please echo hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != provider.StopEndTurn {
		t.Errorf("StopReason = %q, want %q", res.StopReason, provider.StopEndTurn)
	}
	if res.Iterations != 2 {
		t.Errorf("Iterations = %d, want 2", res.Iterations)
	}
	if res.FinalText != "all done" {
		t.Errorf("FinalText = %q, want %q", res.FinalText, "all done")
	}

	// The log must hold the user message, the tool_call, its result, and the
	// final assistant text, in append order.
	wantKinds := []schema.Kind{schema.KindText, schema.KindToolCall, schema.KindToolResult, schema.KindText}
	var gotKinds []schema.Kind
	var result *schema.ToolResultBody
	for _, b := range h.log.Events() {
		gotKinds = append(gotKinds, b.Kind)
		if b.Kind == schema.KindToolResult {
			result = b.ToolResult
		}
	}
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("log kinds = %v, want %v", gotKinds, wantKinds)
	}
	for i := range wantKinds {
		if gotKinds[i] != wantKinds[i] {
			t.Fatalf("log kinds = %v, want %v", gotKinds, wantKinds)
		}
	}
	if result == nil || resultText(result) != "echo:hi" {
		t.Fatalf("tool result = %+v, want text echo:hi", result)
	}

	// The second turn's request must re-project the log and include the tool
	// result — context is recomputed, never cached.
	reqs := p.Requests()
	if len(reqs) != 2 {
		t.Fatalf("provider saw %d requests, want 2", len(reqs))
	}
	if !contextHasResult(reqs[1].Context, "toolu_1") {
		t.Errorf("second request context missing tool_result for toolu_1")
	}

	// Streaming and tool transparency surfaced as UI events.
	if h.kinds(loop.UIToolStarted) != 1 || h.kinds(loop.UIToolFinished) != 1 {
		t.Errorf("tool UI events: started=%d finished=%d, want 1 each", h.kinds(loop.UIToolStarted), h.kinds(loop.UIToolFinished))
	}
	if h.kinds(loop.UITextDelta) == 0 {
		t.Errorf("expected at least one text delta UI event")
	}
	if h.kinds(loop.UITurnComplete) != 2 {
		t.Errorf("turn complete events = %d, want 2", h.kinds(loop.UITurnComplete))
	}
}

// TestMalformedToolArgsDoesNotCorruptLog verifies that when the model streams
// invalid JSON for a tool call, the assembled block is still serializable: the
// verbatim string is preserved in ArgumentsRaw while Arguments is left unset, so
// the append-only log never holds a block that fails to marshal.
func TestMalformedToolArgsDoesNotCorruptLog(t *testing.T) {
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		return provider.ToolCallTurn("toolu_bad", "echo", json.RawMessage(`{"msg":`)), nil
	}}
	h := newHarness(t, p, []tool.Tool{echoTool()}, loop.WithMaxIterations(2))

	if _, err := h.engine.Run(context.Background(), "send bad args"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var call *schema.Block
	for _, b := range h.log.Events() {
		b := b
		if b.Kind == schema.KindToolCall {
			call = &b
		}
		// Every block on the log must marshal cleanly — the corruption the guard
		// prevents would surface here (and on any disk-backed persist).
		if _, err := json.Marshal(b); err != nil {
			t.Fatalf("block %s failed to marshal: %v", b.ID, err)
		}
	}
	if call == nil || call.ToolCall == nil {
		t.Fatal("no tool_call recorded")
	}
	if call.ToolCall.ArgumentsRaw != `{"msg":` {
		t.Errorf("ArgumentsRaw = %q, want verbatim malformed string", call.ToolCall.ArgumentsRaw)
	}
	if len(call.ToolCall.Arguments) != 0 {
		t.Errorf("Arguments = %q, want unset for malformed JSON", call.ToolCall.Arguments)
	}
	// The malformed call must still be answered (an error result), not orphaned.
	assertNoOrphanCalls(t, h.log)
}

func contextHasResult(ctx []schema.Block, toolUseID string) bool {
	for _, b := range ctx {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil && b.ToolResult.ToolUseID == toolUseID {
			return true
		}
	}
	return false
}

// TestMaxIterationGuard verifies a model that never stops asking for tools is
// halted by the safety valve with a clear stop reason rather than looping
// forever.
func TestMaxIterationGuard(t *testing.T) {
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		return provider.ToolCallTurn("toolu_x", "echo", json.RawMessage(`{"msg":"again"}`)), nil
	}}
	h := newHarness(t, p, []tool.Tool{echoTool()}, loop.WithMaxIterations(3))

	res, err := h.engine.Run(context.Background(), "loop forever")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != loop.StopMaxIterations {
		t.Errorf("StopReason = %q, want %q", res.StopReason, loop.StopMaxIterations)
	}
	if res.Iterations != 3 {
		t.Errorf("Iterations = %d, want 3", res.Iterations)
	}
	// Each of the 3 turns is a distinct tool call; none must be left without a
	// result on the log.
	assertNoOrphanCalls(t, h.log)
}

// TestRetryThenSucceed checks the loop retries a retryable provider error and
// then completes, without duplicating the eventual turn's blocks.
func TestRetryThenSucceed(t *testing.T) {
	attempts := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		attempts++
		if attempts == 1 {
			return nil, provider.New(provider.ErrOverloaded, "temporarily overloaded")
		}
		return provider.TextTurn("recovered", ""), nil
	}}
	h := newHarness(t, p, nil, loop.WithRetry(3, time.Millisecond))

	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FinalText != "recovered" {
		t.Errorf("FinalText = %q, want recovered", res.FinalText)
	}
	if attempts != 2 {
		t.Errorf("provider attempts = %d, want 2", attempts)
	}
	// One user block + one assistant text block — the failed attempt appended
	// nothing.
	if got := h.log.Len(); got != 2 {
		t.Errorf("log length = %d, want 2 (no duplicate blocks)", got)
	}
}

// TestNonRetryableErrorSurfaces verifies a non-retryable provider error ends the
// run immediately with the error, not a spin.
func TestNonRetryableErrorSurfaces(t *testing.T) {
	attempts := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		attempts++
		return nil, provider.New(provider.ErrAuth, "bad key")
	}}
	h := newHarness(t, p, nil, loop.WithRetry(5, time.Millisecond))

	_, err := h.engine.Run(context.Background(), "go")
	if err == nil {
		t.Fatal("Run returned nil error, want auth error")
	}
	if provider.KindOf(err) != provider.ErrAuth {
		t.Errorf("error kind = %q, want auth", provider.KindOf(err))
	}
	if attempts != 1 {
		t.Errorf("provider attempts = %d, want 1 (no retry on auth)", attempts)
	}
}

// TestCancelMidToolLeavesConsistentLog cancels the context while a tool is
// executing; the loop must reconcile the in-flight tool_call with a cancellation
// marker so no call is left orphaned.
func TestCancelMidToolLeavesConsistentLog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hang := tool.Func{
		Spec: tool.Def{Name: "hang", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Fn: func(ctx context.Context, _ json.RawMessage) (tool.Output, error) {
			cancel() // cancel the run while inside the tool
			<-ctx.Done()
			return tool.Output{}, ctx.Err()
		},
	}
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		return provider.ToolCallTurn("toolu_hang", "hang", json.RawMessage(`{}`)), nil
	}}
	h := newHarness(t, p, []tool.Tool{hang})

	res, err := h.engine.Run(ctx, "hang please")
	if err == nil {
		t.Fatal("Run returned nil error, want context cancellation")
	}
	if res.StopReason != loop.StopCanceled {
		t.Errorf("StopReason = %q, want %q", res.StopReason, loop.StopCanceled)
	}
	assertNoOrphanCalls(t, h.log)
	assertCancellationMarker(t, h.log, "toolu_hang")
}

// TestCancelMidStreamLeavesConsistentLog cancels the context after a tool_call
// has streamed onto the log but before the loop dispatches it. The abandoned
// call must still be reconciled with a cancellation marker.
func TestCancelMidStreamLeavesConsistentLog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The script streams a complete tool_call, then opens a text block whose
	// first delta triggers the cancel; the next stream read observes the
	// cancellation and ends the turn.
	events := []provider.Event{
		{Type: provider.EventTurnStart, Turn: &provider.TurnInfo{}},
		{Type: provider.EventBlockStart, BlockIndex: 0, Header: &provider.BlockHeader{
			Kind: schema.KindToolCall, Role: schema.RoleAssistant,
			ToolUseID: "toolu_mid", ToolName: "echo", ToolKind: schema.ToolKindClient,
		}},
		{Type: provider.EventToolCallDelta, BlockIndex: 0, ArgumentsDelta: `{"msg":"x"}`},
		{Type: provider.EventBlockStop, BlockIndex: 0},
		{Type: provider.EventBlockStart, BlockIndex: 1, Header: &provider.BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}},
		{Type: provider.EventTextDelta, BlockIndex: 1, TextDelta: "cancel-now"},
	}
	p := &ctxProvider{events: events}
	h := newHarness(t, p, []tool.Tool{echoTool()})
	h.onEvent = func(ev loop.UIEvent) {
		if ev.Kind == loop.UITextDelta && ev.Text == "cancel-now" {
			cancel()
		}
	}

	res, err := h.engine.Run(ctx, "go")
	if err == nil {
		t.Fatal("Run returned nil error, want context cancellation")
	}
	if res.StopReason != loop.StopCanceled {
		t.Errorf("StopReason = %q, want %q", res.StopReason, loop.StopCanceled)
	}
	assertNoOrphanCalls(t, h.log)
	assertCancellationMarker(t, h.log, "toolu_mid")
}

// TestNewValidatesDependencies checks the constructor rejects missing
// dependencies rather than failing mid-turn.
func TestNewValidatesDependencies(t *testing.T) {
	lg := eventlog.New()
	reg := tool.NewRegistry()
	rt := tool.NewRuntime(reg, lg)
	p := &provider.Mock{}

	cases := map[string]func() (*loop.Engine, error){
		"nil provider": func() (*loop.Engine, error) { return loop.New(nil, lg, rt, reg, "m") },
		"nil log":      func() (*loop.Engine, error) { return loop.New(p, nil, rt, reg, "m") },
		"nil runtime":  func() (*loop.Engine, error) { return loop.New(p, lg, nil, reg, "m") },
		"nil registry": func() (*loop.Engine, error) { return loop.New(p, lg, rt, nil, "m") },
		"empty model":  func() (*loop.Engine, error) { return loop.New(p, lg, rt, reg, "") },
	}
	for name, fn := range cases {
		if _, err := fn(); err == nil {
			t.Errorf("%s: New returned nil error, want failure", name)
		}
	}
}

// TestLoopHasNoTUIImports enforces the AS-018 layering rule: the loop is
// face-agnostic, so it must not import any UI/face package.
func TestLoopHasNoTUIImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	banned := []string{"/tui", "bubbletea", "tcell", "/face"}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		// Test files may import faces; the loop package itself must not.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if strings.Contains(path, b) {
					t.Errorf("%s imports forbidden face package %q", name, path)
				}
			}
		}
	}
}

// assertNoOrphanCalls fails if any tool_call on the log lacks a matching
// tool_result — the core consistency invariant of AS-018.
func assertNoOrphanCalls(t *testing.T, lg *eventlog.Log) {
	t.Helper()
	results := map[string]bool{}
	for _, b := range lg.Events() {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil {
			results[b.ToolResult.ToolUseID] = true
		}
	}
	for _, b := range lg.Events() {
		if b.Kind == schema.KindToolCall && b.ToolCall != nil && !results[b.ToolCall.ToolUseID] {
			t.Errorf("orphaned tool_call %q has no result", b.ToolCall.ToolUseID)
		}
	}
}

// assertCancellationMarker fails unless the log carries an error tool_result for
// toolUseID — the cancellation marker the loop appends for an abandoned call.
func assertCancellationMarker(t *testing.T, lg *eventlog.Log, toolUseID string) {
	t.Helper()
	for _, b := range lg.Events() {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil &&
			b.ToolResult.ToolUseID == toolUseID && b.ToolResult.IsError {
			return
		}
	}
	t.Errorf("no cancellation marker tool_result for %q", toolUseID)
}

// ctxProvider streams a fixed event slice through a context-aware stream: each
// advance first checks whether the run's context was cancelled, modeling a real
// provider that stops streaming when the caller cancels.
type ctxProvider struct {
	events []provider.Event
}

func (p *ctxProvider) Name() string { return "ctxmock" }

func (p *ctxProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	return &ctxStream{ctx: ctx, events: p.events}, nil
}

type ctxStream struct {
	ctx    context.Context
	events []provider.Event
	i      int
	cur    provider.Event
	err    error
}

func (s *ctxStream) Next() bool {
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return false
	}
	if s.i >= len(s.events) {
		return false
	}
	s.cur = s.events[s.i]
	s.i++
	return true
}

func (s *ctxStream) Event() provider.Event { return s.cur }
func (s *ctxStream) Err() error            { return s.err }
func (s *ctxStream) Close() error          { return nil }
