package tool

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// callBlock builds a tool_call block for tool name with the given raw arguments.
func callBlock(name, args string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindToolCall,
		Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{
			ToolUseID: "tu_" + name,
			Name:      name,
			Arguments: json.RawMessage(args),
		},
	}
}

// echoTool returns its "msg" argument as text. recorded, when non-nil, is closed
// (well, sent to) each time Run executes, so a test can assert execution.
func echoTool(name string, ran *bool) Tool {
	return Func{
		Spec: Def{
			Name:        name,
			InputSchema: json.RawMessage(`{"type":"object","required":["msg"],"properties":{"msg":{"type":"string"}}}`),
		},
		Fn: func(_ context.Context, args json.RawMessage) (Output, error) {
			if ran != nil {
				*ran = true
			}
			var in struct {
				Msg string `json:"msg"`
			}
			_ = json.Unmarshal(args, &in)
			return Output{Text: in.Msg}, nil
		},
	}
}

func newTestRuntime(t *testing.T, tools ...Tool) (*Runtime, *eventlog.Log) {
	t.Helper()
	reg := NewRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("Register %s: %v", tl.Def().Name, err)
		}
	}
	log := eventlog.New()
	return NewRuntime(reg, log), log
}

// resultFor returns the single tool_result on the log, failing if there isn't
// exactly one.
func resultFor(t *testing.T, log *eventlog.Log) schema.Block {
	t.Helper()
	var results []schema.Block
	for _, b := range log.Events() {
		if b.Kind == schema.KindToolResult {
			results = append(results, b)
		}
	}
	if len(results) != 1 {
		t.Fatalf("want exactly one tool_result, got %d", len(results))
	}
	return results[0]
}

func TestExecuteHappyPathLogsPair(t *testing.T) {
	rt, log := newTestRuntime(t, echoTool("echo", nil))

	call := callBlock("echo", `{"msg":"hello"}`)
	result, err := rt.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// A tool_call + tool_result pair, linked by tool_use_id and provenance.
	if _, ok := log.ByID(call.ID); !ok {
		t.Fatalf("tool_call not on the log")
	}
	if result.Kind != schema.KindToolResult {
		t.Fatalf("result kind = %q, want tool_result", result.Kind)
	}
	if result.ToolResult.ToolUseID != call.ToolCall.ToolUseID {
		t.Fatalf("result ToolUseID = %q, want %q", result.ToolResult.ToolUseID, call.ToolCall.ToolUseID)
	}
	if result.Provenance == nil || len(result.Provenance.DerivedFrom) != 1 || result.Provenance.DerivedFrom[0] != call.ID {
		t.Fatalf("result provenance does not link to the call: %+v", result.Provenance)
	}
	if result.Attribution == nil || result.Attribution.Tool != "echo" {
		t.Fatalf("result attribution = %+v, want tool=echo", result.Attribution)
	}
	if result.ToolResult.IsError {
		t.Fatalf("result unexpectedly marked error")
	}
	if got := result.ToolResult.Content[0].Text; got != "hello" {
		t.Fatalf("result text = %q, want hello", got)
	}
}

func TestExecuteUnknownToolIsModelReadableError(t *testing.T) {
	rt, log := newTestRuntime(t) // empty registry

	result, err := rt.Execute(context.Background(), callBlock("ghost", `{}`))
	if err != nil {
		t.Fatalf("Execute returned a Go error for an unknown tool: %v", err)
	}
	if !result.ToolResult.IsError {
		t.Fatalf("unknown-tool result not marked error")
	}
	if got := result.ToolResult.Content[0].Text; !strings.Contains(got, "unknown tool") {
		t.Fatalf("error text = %q, want unknown-tool message", got)
	}
	_ = resultFor(t, log) // exactly one result logged
}

func TestExecuteInvalidArgumentsDoesNotCrashOrRun(t *testing.T) {
	var ran bool
	rt, _ := newTestRuntime(t, echoTool("echo", &ran))

	// "msg" is required and must be a string; supply neither.
	result, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":5}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ran {
		t.Fatalf("tool ran despite invalid arguments")
	}
	if !result.ToolResult.IsError {
		t.Fatalf("invalid-args result not marked error")
	}
	if got := result.ToolResult.Content[0].Text; !strings.Contains(got, "invalid arguments") {
		t.Fatalf("error text = %q, want invalid-arguments message", got)
	}
}

func TestExecuteNonToolCallBlockIsGoError(t *testing.T) {
	rt, _ := newTestRuntime(t, echoTool("echo", nil))
	text := schema.Block{ID: schema.NewID(), Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}}
	if _, err := rt.Execute(context.Background(), text); err == nil {
		t.Fatalf("Execute(text block): want Go error, got nil")
	}
}

// TestPermissionHookInvokedOnEveryExecution enforces that no tool runs without
// passing the permission gate, and that a denial produces a model-readable error
// without executing the tool.
func TestPermissionHookInvokedOnEveryExecution(t *testing.T) {
	var ran bool
	reg := NewRegistry()
	if err := reg.Register(echoTool("echo", &ran)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var mu sync.Mutex
	var seen []string
	deny := false
	perm := func(_ context.Context, c Call) Decision {
		mu.Lock()
		seen = append(seen, c.Name)
		mu.Unlock()
		if deny {
			return Denied("not allowed here")
		}
		return Allowed()
	}
	rt := NewRuntime(reg, eventlog.New(), WithPermission(perm))

	// Allowed: gate consulted, tool runs.
	if _, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":"x"}`)); err != nil {
		t.Fatalf("Execute (allowed): %v", err)
	}
	if !ran {
		t.Fatalf("tool did not run when permitted")
	}
	if len(seen) != 1 || seen[0] != "echo" {
		t.Fatalf("permission hook not consulted: seen=%v", seen)
	}

	// Denied: gate consulted, tool does NOT run, error result returned.
	ran = false
	deny = true
	result, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":"y"}`))
	if err != nil {
		t.Fatalf("Execute (denied): %v", err)
	}
	if ran {
		t.Fatalf("tool ran despite denied permission")
	}
	if !result.ToolResult.IsError || !strings.Contains(result.ToolResult.Content[0].Text, "permission denied") {
		t.Fatalf("denied result = %+v, want permission-denied error", result.ToolResult)
	}
	if len(seen) != 2 {
		t.Fatalf("permission hook consulted %d times, want 2", len(seen))
	}
}

func TestExecutePermissionNotCheckedForInvalidArgs(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(echoTool("echo", nil)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	checked := false
	rt := NewRuntime(reg, eventlog.New(), WithPermission(func(context.Context, Call) Decision {
		checked = true
		return Allowed()
	}))
	if _, err := rt.Execute(context.Background(), callBlock("echo", `{}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if checked {
		t.Fatalf("permission checked for a call that failed validation")
	}
}

func TestExecutePerToolTimeoutIsModelReadable(t *testing.T) {
	slow := Func{
		Spec: Def{Name: "slow", Timeout: 20 * time.Millisecond},
		Fn: func(ctx context.Context, _ json.RawMessage) (Output, error) {
			<-ctx.Done() // honor cancellation: block until the budget elapses
			return Output{}, ctx.Err()
		},
	}
	rt, _ := newTestRuntime(t, slow)
	result, err := rt.Execute(context.Background(), callBlock("slow", `{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.ToolResult.IsError || !strings.Contains(result.ToolResult.Content[0].Text, "timed out") {
		t.Fatalf("timeout result = %+v, want timed-out error", result.ToolResult)
	}
}

func TestExecuteParentCancellationPropagates(t *testing.T) {
	block := Func{
		Spec: Def{Name: "block"},
		Fn: func(ctx context.Context, _ json.RawMessage) (Output, error) {
			<-ctx.Done()
			return Output{}, ctx.Err()
		},
	}
	rt, log := newTestRuntime(t, block)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := rt.Execute(ctx, callBlock("block", `{}`))
	if err == nil {
		t.Fatalf("Execute: want cancellation error, got nil")
	}
	// The turn is abandoned: no tool_result recorded.
	for _, b := range log.Events() {
		if b.Kind == schema.KindToolResult {
			t.Fatalf("a tool_result was recorded for a cancelled turn")
		}
	}
}

func TestExecuteToolDomainErrorRecorded(t *testing.T) {
	failing := Func{
		Spec: Def{Name: "fail"},
		Fn: func(context.Context, json.RawMessage) (Output, error) {
			return Output{Text: "file not found", IsError: true}, nil
		},
	}
	rt, _ := newTestRuntime(t, failing)
	result, err := rt.Execute(context.Background(), callBlock("fail", `{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.ToolResult.IsError || result.ToolResult.Content[0].Text != "file not found" {
		t.Fatalf("domain-error result = %+v", result.ToolResult)
	}
}

func TestExecuteRunErrorRecorded(t *testing.T) {
	// A plain (non-context) error from Run with an uncancelled parent must be
	// recorded as a model-readable failure, not propagated.
	failing := Func{
		Spec: Def{Name: "boom"},
		Fn: func(context.Context, json.RawMessage) (Output, error) {
			return Output{}, errBoom
		},
	}
	rt, _ := newTestRuntime(t, failing)
	result, err := rt.Execute(context.Background(), callBlock("boom", `{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.ToolResult.IsError || !strings.Contains(result.ToolResult.Content[0].Text, "boom") {
		t.Fatalf("run-error result = %+v, want failure message", result.ToolResult)
	}
}

func TestExecuteTruncatesOversizedOutput(t *testing.T) {
	big := strings.Repeat("a", 100)
	huge := Func{
		Spec: Def{Name: "huge"},
		Fn: func(context.Context, json.RawMessage) (Output, error) {
			return Output{Text: big}, nil
		},
	}
	reg := NewRegistry()
	if err := reg.Register(huge); err != nil {
		t.Fatalf("Register: %v", err)
	}
	rt := NewRuntime(reg, eventlog.New(), WithMaxResultBytes(10))
	result, err := rt.Execute(context.Background(), callBlock("huge", `{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.ToolResult.Truncated {
		t.Fatalf("result not marked truncated")
	}
	body := result.ToolResult.Content
	if got := body[0].Text; len(got) != 10 {
		t.Fatalf("truncated text len = %d, want 10", len(got))
	}
	marker := body[len(body)-1].Text
	if !strings.Contains(marker, "truncated") || !strings.Contains(marker, "100") {
		t.Fatalf("truncation marker = %q, want byte counts", marker)
	}
}

func TestExecuteIdempotentWithPreloggedCall(t *testing.T) {
	rt, log := newTestRuntime(t, echoTool("echo", nil))
	call := callBlock("echo", `{"msg":"hi"}`)
	// Simulate the loop having already recorded the assistant turn's tool_call.
	if _, err := log.Append(call); err != nil {
		t.Fatalf("pre-append call: %v", err)
	}
	if _, err := rt.Execute(context.Background(), call); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Exactly one tool_call (not re-appended) and one tool_result.
	var calls, results int
	for _, b := range log.Events() {
		switch b.Kind {
		case schema.KindToolCall:
			calls++
		case schema.KindToolResult:
			results++
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("log has %d calls / %d results, want 1 / 1", calls, results)
	}
}

var errBoom = boomError{}

type boomError struct{}

func (boomError) Error() string { return "boom" }
