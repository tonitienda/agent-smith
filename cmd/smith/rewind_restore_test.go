package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/snapshot"
	"github.com/tonitienda/agent-smith/schema"
)

func toolCall(id string, seq int, name, useID, path string) schema.Block {
	args, _ := json.Marshal(map[string]string{"path": path})
	return schema.Block{
		ID:       id,
		Kind:     schema.KindToolCall,
		Seq:      seq,
		ToolCall: &schema.ToolCallBody{Name: name, ToolUseID: useID, Arguments: args},
	}
}

// TestDroppedFileMutations extracts only the dropped write/edit calls, keyed by
// tool_use id and carrying the block sequence.
func TestDroppedFileMutations(t *testing.T) {
	events := []schema.Block{
		toolCall("blk_keep", 1, "write", "tuKeep", "kept.txt"), // not in dropIDs
		toolCall("blk_w", 2, "write", "tuW", "a.txt"),
		toolCall("blk_grep", 3, "grep", "tuG", "ignored"), // not a file mutation
		toolCall("blk_e", 4, "edit", "tuE", "b.txt"),
		{ID: "blk_text", Kind: schema.KindText, Seq: 5}, // not a tool call
	}
	got := droppedFileMutations(events, []string{"blk_w", "blk_e", "blk_grep", "blk_text"})
	if len(got) != 2 {
		t.Fatalf("want 2 mutations, got %+v", got)
	}
	if got[0].ToolUseID != "tuW" || got[0].Seq != 2 {
		t.Fatalf("first mutation = %+v", got[0])
	}
	if got[1].ToolUseID != "tuE" || got[1].Seq != 4 {
		t.Fatalf("second mutation = %+v", got[1])
	}
}

// TestUncoveredFiles flags modified files the restore plan does not cover (no
// snapshot captured for them).
func TestUncoveredFiles(t *testing.T) {
	actions := []snapshot.FileAction{{Path: "a.txt", Kind: snapshot.ActionRestore}}
	got := uncoveredFiles(actions, []string{"a.txt", "b.txt"})
	if len(got) != 1 || got[0] != "b.txt" {
		t.Fatalf("uncovered = %+v, want [b.txt]", got)
	}
}

// TestRenderRestorePlan groups actions and the no-snapshot files into a readable
// preview, showing only the categories that apply.
func TestRenderRestorePlan(t *testing.T) {
	actions := []snapshot.FileAction{
		{Path: "r.txt", Kind: snapshot.ActionRestore},
		{Path: "d.txt", Kind: snapshot.ActionDelete},
		{Path: "c.txt", Kind: snapshot.ActionConflict},
		{Path: "l.txt", Kind: snapshot.ActionLargeSkip},
	}
	out := renderRestorePlan(actions, []string{"r.txt", "d.txt", "c.txt", "l.txt", "n.txt"})
	for _, want := range []string{"r.txt", "d.txt", "c.txt", "l.txt", "n.txt", "restore", "delete", "changed outside Smith", "too large", "no snapshot"} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview missing %q:\n%s", want, out)
		}
	}
}

// TestRenderRestorePlanEmpty reports clearly when there is nothing to restore.
func TestRenderRestorePlanEmpty(t *testing.T) {
	out := renderRestorePlan(nil, nil)
	if !strings.Contains(out, "no snapshotted file changes") {
		t.Fatalf("want empty notice, got %q", out)
	}
}
