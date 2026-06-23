package builtin

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/tonitienda/agent-smith/internal/tool"
)

// capturedCall records one Snapshotter.Capture invocation for assertions.
type capturedCall struct {
	toolUseID string
	relPath   string
	pre       string
	preExists bool
	post      string
}

type fakeSnapshotter struct{ calls []capturedCall }

// Capture reads abs itself — as the real store does — which also proves the hook
// fires before the tool mutates the file (it still sees the pre-state).
func (f *fakeSnapshotter) Capture(id, rel, abs string, post []byte) error {
	pre, err := os.ReadFile(abs)
	f.calls = append(f.calls, capturedCall{id, rel, string(pre), err == nil, string(post)})
	return nil
}

// runWithCall invokes a tool with a tool_use id on the context, as the runtime
// does, so the snapshot hook can correlate the call.
func runWithCall(t *testing.T, f *FS, name, id, args string) tool.Output {
	t.Helper()
	ctx := tool.ContextWithToolUseID(context.Background(), id)
	out, err := byName(t, f, name).Run(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s.Run returned a Go error: %v", name, err)
	}
	return out
}

// TestSnapshotOnWriteNewFile: writing a new file captures an absent pre-state
// before the write (AS-084 AC1).
func TestSnapshotOnWriteNewFile(t *testing.T) {
	snap := &fakeSnapshotter{}
	f := newFS(t, nil, WithSnapshotter(snap))

	out := runWithCall(t, f, "write", "tu1", `{"path":"new.txt","content":"hello"}`)
	if out.IsError {
		t.Fatalf("write error: %s", out.Text)
	}
	if len(snap.calls) != 1 {
		t.Fatalf("want one capture, got %d", len(snap.calls))
	}
	c := snap.calls[0]
	if c.toolUseID != "tu1" || c.relPath != "new.txt" || c.preExists || c.pre != "" || c.post != "hello" {
		t.Fatalf("unexpected capture: %+v", c)
	}
}

// TestSnapshotOnEditExistingFile: editing captures the file's pre-edit content
// and the post-edit content before the write lands.
func TestSnapshotOnEditExistingFile(t *testing.T) {
	snap := &fakeSnapshotter{}
	f := newFS(t, map[string]string{"a.txt": "foo bar"}, WithSnapshotter(snap))

	out := runWithCall(t, f, "edit", "tu2", `{"path":"a.txt","old_string":"foo","new_string":"baz"}`)
	if out.IsError {
		t.Fatalf("edit error: %s", out.Text)
	}
	if len(snap.calls) != 1 {
		t.Fatalf("want one capture, got %d", len(snap.calls))
	}
	c := snap.calls[0]
	if !c.preExists || c.pre != "foo bar" || c.post != "baz bar" {
		t.Fatalf("unexpected capture: %+v", c)
	}
}

// TestSnapshotSkippedWithoutToolUseID: a call with no tool_use id on the context
// (a tool invoked outside the runtime) records nothing.
func TestSnapshotSkippedWithoutToolUseID(t *testing.T) {
	snap := &fakeSnapshotter{}
	f := newFS(t, nil, WithSnapshotter(snap))
	run(t, f, "write", `{"path":"x.txt","content":"hi"}`) // run() uses a bare context
	if len(snap.calls) != 0 {
		t.Fatalf("want no captures without a tool_use id, got %d", len(snap.calls))
	}
}
