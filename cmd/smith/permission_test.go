package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/permission"
)

func TestDestructiveOnlyShell(t *testing.T) {
	if !destructive("shell") {
		t.Fatal("shell should escalate to the blocking modal")
	}
	for _, tool := range []string{"read", "write", "edit", "glob", "grep"} {
		if destructive(tool) {
			t.Errorf("%q should use the inline card, not the modal", tool)
		}
	}
}

func TestEditDiffRendersReplacement(t *testing.T) {
	args, _ := json.Marshal(map[string]string{
		"path":       "main.go",
		"old_string": "a\nb",
		"new_string": "a\nc",
	})
	got := editDiff(permission.Request{Tool: "edit", Arguments: args})
	want := "- a\n- b\n+ a\n+ c"
	if got != want {
		t.Fatalf("editDiff = %q, want %q", got, want)
	}
}

func TestEditDiffTrimsTrailingNewline(t *testing.T) {
	args, _ := json.Marshal(map[string]string{
		"old_string": "a\n",
		"new_string": "b\n",
	})
	got := editDiff(permission.Request{Tool: "edit", Arguments: args})
	if want := "- a\n+ b"; got != want {
		t.Fatalf("editDiff = %q, want %q (no spurious empty diff line)", got, want)
	}
}

func TestEditDiffTruncatesHugeEdits(t *testing.T) {
	var big strings.Builder
	for i := 0; i < 500; i++ {
		big.WriteString("line\n")
	}
	args, _ := json.Marshal(map[string]string{"old_string": big.String(), "new_string": "x"})
	got := editDiff(permission.Request{Tool: "edit", Arguments: args})
	lines := strings.Count(got, "\n") + 1
	if lines > maxDiffLines+1 {
		t.Fatalf("diff has %d lines, want <= %d", lines, maxDiffLines+1)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("truncated diff missing marker:\n%s", got)
	}
}

func TestEditDiffOnlyForEdits(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"command": "ls"})
	if got := editDiff(permission.Request{Tool: "shell", Arguments: args}); got != "" {
		t.Fatalf("editDiff for shell = %q, want empty", got)
	}
}

func TestEditDiffToleratesBadArgs(t *testing.T) {
	if got := editDiff(permission.Request{Tool: "edit", Arguments: []byte("not json")}); got != "" {
		t.Fatalf("editDiff with bad args = %q, want empty", got)
	}
	if !strings.HasPrefix(editDiff(permission.Request{Tool: "edit", Arguments: []byte(`{"old_string":"x","new_string":"y"}`)}), "- x") {
		t.Fatal("editDiff should render even with no path")
	}
}
