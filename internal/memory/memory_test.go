package memory_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/memory"
	"github.com/tonitienda/agent-smith/schema"
)

// write creates a file with content under dir, creating parents as needed.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestEquivalence is AC1: a project with only CLAUDE.md behaves identically to
// the same project with only AGENT.md (and AGENTS.md) — the three filenames are
// equivalent, so the loaded content is the same regardless of which is used.
func TestEquivalence(t *testing.T) {
	const body = "always run the tests before committing"
	for _, name := range memory.Filenames {
		t.Run(name, func(t *testing.T) {
			wd := t.TempDir()
			write(t, filepath.Join(wd, name), body)

			blocks, err := memory.Load("", wd)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(blocks) != 1 {
				t.Fatalf("got %d blocks, want 1", len(blocks))
			}
			if blocks[0].Text == nil || blocks[0].Text.Text != body {
				t.Fatalf("content = %+v, want %q", blocks[0].Text, body)
			}
			if blocks[0].Role != schema.RoleSystem || blocks[0].Kind != schema.KindText {
				t.Fatalf("kind/role = %s/%s, want text/system", blocks[0].Kind, blocks[0].Role)
			}
		})
	}
}

// TestHierarchyPrecedence is AC2: discovery is deterministic, user → project →
// directory, lowest precedence first, with the nearer (deeper) file last.
func TestHierarchyPrecedence(t *testing.T) {
	userDir := t.TempDir()
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	write(t, filepath.Join(userDir, "AGENTS.md"), "user")
	write(t, filepath.Join(root, "CLAUDE.md"), "project")
	write(t, filepath.Join(sub, "AGENT.md"), "dir")

	paths := memory.Discover(userDir, sub)
	got := make([]string, len(paths))
	for i, p := range paths {
		got[i] = filepath.Base(p)
	}
	want := []string{"AGENTS.md", "CLAUDE.md", "AGENT.md"} // user → project → dir
	if len(got) != len(want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestDeterministicOrderWithinDir checks that when several filenames coexist in
// one directory they load in the documented Filenames order.
func TestDeterministicOrderWithinDir(t *testing.T) {
	wd := t.TempDir()
	for _, name := range memory.Filenames {
		write(t, filepath.Join(wd, name), name)
	}
	paths := memory.Discover("", wd)
	if len(paths) != len(memory.Filenames) {
		t.Fatalf("discovered %d files, want %d", len(paths), len(memory.Filenames))
	}
	for i, p := range paths {
		if filepath.Base(p) != memory.Filenames[i] {
			t.Fatalf("order[%d] = %s, want %s", i, filepath.Base(p), memory.Filenames[i])
		}
	}
}

// TestSkipsEmpty checks that whitespace-only files add no segment.
func TestSkipsEmpty(t *testing.T) {
	wd := t.TempDir()
	write(t, filepath.Join(wd, "CLAUDE.md"), "   \n\t\n")
	blocks, err := memory.Load("", wd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("got %d blocks, want 0 for an empty file", len(blocks))
	}
}

// TestSourceRoundTrip checks the Origin attribution helper: a memory block
// reports its source path, and a non-memory block does not.
func TestSourceRoundTrip(t *testing.T) {
	const path = "/proj/CLAUDE.md"
	b := memory.Block(path, "body")
	got, ok := memory.Source(b)
	if !ok || got != path {
		t.Fatalf("Source = %q,%v want %q,true", got, ok, path)
	}

	other := schema.Block{Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hi"}}
	if _, ok := memory.Source(other); ok {
		t.Fatal("Source reported a non-memory block as memory")
	}
}
