package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// runtimeWith builds a Runtime over one echo tool plus the given options, so a
// hook test can wire WithPreToolHook/WithPostToolHook.
func runtimeWith(t *testing.T, ran *bool, opts ...Option) (*Runtime, *eventlog.Log) {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Register(echoTool("echo", ran)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	log := eventlog.New()
	return NewRuntime(reg, log, opts...), log
}

// TestPreToolHookBlocksExecution covers AS-035 AC: a blocking pre-tool-use hook
// prevents execution and the model receives the reason in the error result.
func TestPreToolHookBlocksExecution(t *testing.T) {
	ran := false
	rt, log := runtimeWith(t, &ran, WithPreToolHook(func(context.Context, Call) PreToolResult {
		return PreToolResult{Block: true, Reason: "policy says no"}
	}))

	res, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ran {
		t.Fatal("tool ran despite a blocking hook")
	}
	if res.ToolResult == nil || !res.ToolResult.IsError {
		t.Fatal("blocked call should record an error result")
	}
	if got := res.ToolResult.Content[0].Text; !strings.Contains(got, "policy says no") {
		t.Fatalf("model should see the block reason, got %q", got)
	}
	_ = log
}

// TestPreToolHookModifiesArguments covers AS-035 AC: a modify hook altering tool
// input is applied (the tool runs with the new args) and visible on the log as a
// derived tool_call with hook provenance.
func TestPreToolHookModifiesArguments(t *testing.T) {
	ran := false
	rt, log := runtimeWith(t, &ran, WithPreToolHook(func(_ context.Context, c Call) PreToolResult {
		var in struct {
			Msg string `json:"msg"`
		}
		_ = json.Unmarshal(c.Arguments, &in)
		return PreToolResult{Modified: json.RawMessage(`{"msg":"rewritten"}`)}
	}))

	res, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":"original"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !ran {
		t.Fatal("tool should still run after a modify hook")
	}
	// The echo tool returns its msg; the rewritten value proves the modification
	// reached execution.
	if got := res.ToolResult.Content[0].Text; got != "rewritten" {
		t.Fatalf("tool ran with un-rewritten args, got %q", got)
	}

	// The modification is visible on the log: a derived tool_call attributed to
	// the hook, deriving from the original call.
	var derived *schema.Block
	for i, b := range log.Events() {
		if b.Kind == schema.KindToolCall && b.Provenance != nil && b.Provenance.Producer == "hook" {
			derived = &log.Events()[i]
		}
	}
	if derived == nil {
		t.Fatal("no hook-derived tool_call on the log")
	}
	if string(derived.ToolCall.Arguments) != `{"msg":"rewritten"}` {
		t.Fatalf("derived call has wrong args: %s", derived.ToolCall.Arguments)
	}
	if len(derived.Provenance.DerivedFrom) != 1 {
		t.Fatalf("derived call should derive from the original, got %v", derived.Provenance.DerivedFrom)
	}
}

// TestPreToolHookModifyInvalidArgsBlocks asserts a hook cannot smuggle a
// schema-invalid rewrite past validation: the call is recorded as an error and
// never runs.
func TestPreToolHookModifyInvalidArgsBlocks(t *testing.T) {
	ran := false
	rt, _ := runtimeWith(t, &ran, WithPreToolHook(func(context.Context, Call) PreToolResult {
		return PreToolResult{Modified: json.RawMessage(`{"msg":123}`)} // msg must be a string
	}))

	res, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":"ok"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ran {
		t.Fatal("tool ran with hook-supplied invalid args")
	}
	if res.ToolResult == nil || !res.ToolResult.IsError {
		t.Fatal("invalid rewrite should record an error result")
	}
}

// TestPostToolHookFires covers AS-035: the post-tool-use hook observes the
// executed call and its recorded result.
func TestPostToolHookFires(t *testing.T) {
	var gotName string
	var gotResult *schema.ToolResultBody
	rt, _ := runtimeWith(t, nil, WithPostToolHook(func(_ context.Context, c Call, r *schema.ToolResultBody) {
		gotName = c.Name
		gotResult = r
	}))

	if _, err := rt.Execute(context.Background(), callBlock("echo", `{"msg":"hi"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotName != "echo" {
		t.Fatalf("post hook saw wrong tool %q", gotName)
	}
	if gotResult == nil || gotResult.Content[0].Text != "hi" {
		t.Fatalf("post hook did not receive the result: %+v", gotResult)
	}
}

// TestPostToolHookSkippedOnDeny asserts the post hook does not fire for a call
// that never executed (e.g. an unknown tool), since there is no real result to
// observe.
func TestPostToolHookSkippedOnDeny(t *testing.T) {
	fired := false
	rt, _ := runtimeWith(t, nil, WithPostToolHook(func(context.Context, Call, *schema.ToolResultBody) {
		fired = true
	}))

	if _, err := rt.Execute(context.Background(), callBlock("nonexistent", `{}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fired {
		t.Fatal("post hook should not fire for a call that never ran")
	}
}
