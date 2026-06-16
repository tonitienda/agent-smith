package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/command"
)

// parityDocPath locates docs/project/command-parity.md from the cmd/smith test dir.
func parityDocPath() string {
	return filepath.Join("..", "..", "docs", "project", "command-parity.md")
}

// TestCommandParityDocInSync fails if the checked-in parity table drifts from the
// registry (AS-066). Regenerate with `UPDATE_DOCS=1 go test ./cmd/smith`.
func TestCommandParityDocInSync(t *testing.T) {
	want := parityDoc()
	path := parityDocPath()
	if os.Getenv("UPDATE_DOCS") != "" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatalf("write parity doc: %v", err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read parity doc (run `UPDATE_DOCS=1 go test ./cmd/smith`): %v", err)
	}
	if string(got) != want {
		t.Errorf("parity doc is stale; regenerate with `UPDATE_DOCS=1 go test ./cmd/smith`")
	}
}

// TestNoSilentInteractiveOnly asserts every interactive-only built-in states a
// reason (UX.md §17.5, AS-066 acceptance criterion).
func TestNoSilentInteractiveOnly(t *testing.T) {
	for _, c := range chatCommands(nil).All() {
		if c.Scriptability == command.InteractiveOnly && c.Reason == "" {
			t.Errorf("command %q is interactive-only without a stated reason", c.Name)
		}
	}
}
