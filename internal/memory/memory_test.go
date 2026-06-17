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

// blockFor returns the loaded memory block sourced from path, if present. The
// tests key on specific written paths rather than the full result set, because
// Discover walks the working directory's ancestors up to the filesystem root —
// a stray memory file in a real ancestor (e.g. /tmp) must not make a test flaky.
func blockFor(blocks []schema.Block, path string) (schema.Block, bool) {
	for _, b := range blocks {
		if src, ok := memory.Source(b); ok && src == path {
			return b, true
		}
	}
	return schema.Block{}, false
}

// indexOfPath returns the position of path within paths, or -1 when absent.
func indexOfPath(paths []string, path string) int {
	for i, p := range paths {
		if p == path {
			return i
		}
	}
	return -1
}

// TestEquivalence is AC1: a project with only CLAUDE.md behaves identically to
// the same project with only AGENT.md (and AGENTS.md) — the three filenames are
// equivalent, so the loaded content is the same regardless of which is used.
func TestEquivalence(t *testing.T) {
	const body = "always run the tests before committing"
	for _, name := range memory.Filenames {
		t.Run(name, func(t *testing.T) {
			wd := t.TempDir()
			path := filepath.Join(wd, name)
			write(t, path, body)

			blocks, err := memory.Load("", wd)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			got, ok := blockFor(blocks, path)
			if !ok {
				t.Fatalf("no memory block for %s (loaded %d blocks)", path, len(blocks))
			}
			if got.Text == nil || got.Text.Text != body {
				t.Fatalf("content = %+v, want %q", got.Text, body)
			}
			if got.Role != schema.RoleSystem || got.Kind != schema.KindText {
				t.Fatalf("kind/role = %s/%s, want text/system", got.Kind, got.Role)
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

	userPath := filepath.Join(userDir, "AGENTS.md")
	projPath := filepath.Join(root, "CLAUDE.md")
	dirPath := filepath.Join(sub, "AGENT.md")
	write(t, userPath, "user")
	write(t, projPath, "project")
	write(t, dirPath, "dir")

	// Assert the relative order of the three written files (user → project → dir).
	// Discover may also surface memory files from real ancestors of the temp dir,
	// so we check positions rather than the full set.
	paths := memory.Discover(userDir, sub)
	iUser := indexOfPath(paths, userPath)
	iProj := indexOfPath(paths, projPath)
	iDir := indexOfPath(paths, dirPath)
	if iUser < 0 || iProj < 0 || iDir < 0 {
		t.Fatalf("missing expected paths in %v (user=%d proj=%d dir=%d)", paths, iUser, iProj, iDir)
	}
	if iUser >= iProj || iProj >= iDir {
		t.Fatalf("order user=%d proj=%d dir=%d, want ascending (full: %v)", iUser, iProj, iDir, paths)
	}
}

// TestDeterministicOrderWithinDir checks that when several filenames coexist in
// one directory they load in the documented Filenames order.
func TestDeterministicOrderWithinDir(t *testing.T) {
	wd := t.TempDir()
	for _, name := range memory.Filenames {
		write(t, filepath.Join(wd, name), name)
	}
	// Restrict to files in wd itself; Discover may also return files from real
	// ancestor directories of the temp dir.
	var inWd []string
	for _, p := range memory.Discover("", wd) {
		if filepath.Dir(p) == wd {
			inWd = append(inWd, filepath.Base(p))
		}
	}
	if len(inWd) != len(memory.Filenames) {
		t.Fatalf("discovered %v in wd, want %v", inWd, memory.Filenames)
	}
	for i, name := range memory.Filenames {
		if inWd[i] != name {
			t.Fatalf("order[%d] = %s, want %s", i, inWd[i], name)
		}
	}
}

// TestSkipsEmpty checks that whitespace-only files add no segment.
func TestSkipsEmpty(t *testing.T) {
	wd := t.TempDir()
	emptyPath := filepath.Join(wd, "CLAUDE.md")
	write(t, emptyPath, "   \n\t\n")
	blocks, err := memory.Load("", wd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := blockFor(blocks, emptyPath); ok {
		t.Fatalf("whitespace-only file %s should have been skipped", emptyPath)
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
