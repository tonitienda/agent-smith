package builtin_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/tool/builtin"
)

// TestTaskRunSummarizesChild covers the happy path (AS-046): a valid prompt is
// delegated to the Spawner and the child's summary plus session link come back as
// a non-error tool result attributed to the task tool.
func TestTaskRunSummarizesChild(t *testing.T) {
	var got builtin.TaskRequest
	sp := builtin.SpawnerFunc(func(_ context.Context, req builtin.TaskRequest) (builtin.TaskResult, error) {
		got = req
		return builtin.TaskResult{Summary: "did the thing", SessionID: "sess_child"}, nil
	})
	tl := builtin.NewTask(sp)

	out, err := tl.Run(context.Background(), json.RawMessage(`{"prompt":"do it","agent_type":"researcher","model":"m"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error output: %q", out.Text)
	}
	if got.Prompt != "do it" || got.AgentType != "researcher" || got.Model != "m" {
		t.Errorf("spawner got %+v, want prompt/agent_type/model threaded", got)
	}
	if !strings.Contains(out.Text, "did the thing") || !strings.Contains(out.Text, "sess_child") {
		t.Errorf("output %q missing summary or session link", out.Text)
	}
	if out.Attribution == nil || out.Attribution.Tool != "task" {
		t.Errorf("output attribution = %+v, want tool=task", out.Attribution)
	}
}

// TestTaskRunBlankPrompt covers the validation guard: a blank prompt is a
// model-readable error, never a delegation.
func TestTaskRunBlankPrompt(t *testing.T) {
	called := false
	sp := builtin.SpawnerFunc(func(context.Context, builtin.TaskRequest) (builtin.TaskResult, error) {
		called = true
		return builtin.TaskResult{}, nil
	})
	out, err := builtin.NewTask(sp).Run(context.Background(), json.RawMessage(`{"prompt":"   "}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.IsError {
		t.Error("blank prompt should produce an error result")
	}
	if called {
		t.Error("spawner should not be called for a blank prompt")
	}
}

// TestTaskRunSpawnerError covers a delegation failure: it surfaces as a
// model-readable error result so the loop continues, not an infrastructure error.
func TestTaskRunSpawnerError(t *testing.T) {
	sp := builtin.SpawnerFunc(func(context.Context, builtin.TaskRequest) (builtin.TaskResult, error) {
		return builtin.TaskResult{}, errors.New("no provider")
	})
	out, err := builtin.NewTask(sp).Run(context.Background(), json.RawMessage(`{"prompt":"go"}`))
	if err != nil {
		t.Fatalf("Run returned infra error, want model-readable result: %v", err)
	}
	if !out.IsError || !strings.Contains(out.Text, "no provider") {
		t.Errorf("output = %+v, want error result mentioning the failure", out)
	}
}

// TestTaskRunCanceled covers cancellation: when the parent turn is cancelled, the
// spawner error propagates as an infrastructure error so the loop abandons the
// turn rather than feeding the model a spurious result.
func TestTaskRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sp := builtin.SpawnerFunc(func(context.Context, builtin.TaskRequest) (builtin.TaskResult, error) {
		return builtin.TaskResult{}, context.Canceled
	})
	_, err := builtin.NewTask(sp).Run(ctx, json.RawMessage(`{"prompt":"go"}`))
	if err == nil {
		t.Error("a cancelled turn should return an infrastructure error")
	}
}
