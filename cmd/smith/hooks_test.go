package main

import (
	"context"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/hook"
)

// TestLoadHooksFromConfig parses a `hooks` array out of the layered config.
func TestLoadHooksFromConfig(t *testing.T) {
	cfg := config.New(config.MapLayer("project", "test", map[string]any{
		"hooks": []any{
			map[string]any{"event": "session-start", "command": "true"},
			map[string]any{"event": "pre-tool-use", "matcher": "shell", "command": "exit 2"},
		},
	}))
	var stderr strings.Builder
	hooks := loadHooks(cfg, &stderr)
	if !hooks.Has(hook.SessionStart) || !hooks.Has(hook.PreToolUse) {
		t.Fatalf("expected session-start and pre-tool-use hooks")
	}
	if stderr.Len() != 0 {
		t.Fatalf("clean config should warn nothing, got %q", stderr.String())
	}
}

// TestLoadHooksWarnsOnBadSpec surfaces a malformed spec as a stderr warning
// without dropping the whole set.
func TestLoadHooksWarnsOnBadSpec(t *testing.T) {
	cfg := config.New(config.MapLayer("project", "test", map[string]any{
		"hooks": []any{map[string]any{"event": "bogus", "command": "true"}},
	}))
	var stderr strings.Builder
	loadHooks(cfg, &stderr)
	if !strings.Contains(stderr.String(), "unknown event") {
		t.Fatalf("expected unknown-event warning, got %q", stderr.String())
	}
}

// TestRecordHookOutcomeWritesNotes asserts annotations and warnings land on the
// log as hook-note control events the operator can audit.
func TestRecordHookOutcomeWritesNotes(t *testing.T) {
	log := eventlog.New()
	recordHookOutcome(log, hook.PostToolUse, hook.Outcome{
		Annotations: []string{"linted clean"},
		Warnings:    []string{"a hook timed out"},
	})
	var notes []string
	for _, b := range log.Events() {
		if b.Kind == eventlog.KindHookNote && b.Text != nil {
			notes = append(notes, b.Text.Text)
		}
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 hook notes, got %d: %v", len(notes), notes)
	}
	if notes[0] != "linted clean" || !strings.Contains(notes[1], "timed out") {
		t.Fatalf("unexpected note contents: %v", notes)
	}
}

// TestFireLifecycleReturnsBlock confirms the lifecycle helper surfaces a block so
// a caller (prompt-submit) can reject the turn.
func TestFireLifecycleReturnsBlock(t *testing.T) {
	set, _, err := hook.Compile([]hook.Spec{
		{Event: string(hook.UserPromptSubmit), Command: `echo '{"decision":"block","reason":"nope"}'`},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	log := eventlog.New()
	out := fireLifecycle(context.Background(), set, log, hook.Payload{Event: hook.UserPromptSubmit, Prompt: "hi"})
	if !out.Blocked || out.Reason != "nope" {
		t.Fatalf("expected block, got %+v", out)
	}
}
