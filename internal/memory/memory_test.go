package memory_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestImportAttributed is AS-082 AC1: an @import pulls in the target's content
// as its own block, attributed to the imported file's path (not folded into the
// importer's segment).
func TestImportAttributed(t *testing.T) {
	wd := t.TempDir()
	importPath := filepath.Join(wd, "shared.md")
	write(t, importPath, "shared rule")
	mainPath := filepath.Join(wd, "CLAUDE.md")
	write(t, mainPath, "project rule\n@shared.md\n")

	blocks, err := memory.Load("", wd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	main, ok := blockFor(blocks, mainPath)
	if !ok {
		t.Fatalf("no block for importer %s", mainPath)
	}
	if main.Text == nil || main.Text.Text != "project rule\n@shared.md\n" {
		t.Fatalf("importer content = %+v, want it kept intact", main.Text)
	}
	imp, ok := blockFor(blocks, importPath)
	if !ok {
		t.Fatalf("no block attributed to import %s (loaded %d blocks)", importPath, len(blocks))
	}
	if imp.Text == nil || imp.Text.Text != "shared rule" {
		t.Fatalf("import content = %+v, want %q", imp.Text, "shared rule")
	}
}

// TestImportCycleTerminates is AS-082 AC2: a cycle (A imports B, B imports A)
// terminates, loading each file once.
func TestImportCycleTerminates(t *testing.T) {
	wd := t.TempDir()
	aPath := filepath.Join(wd, "CLAUDE.md")
	bPath := filepath.Join(wd, "b.md")
	write(t, aPath, "A\n@b.md\n")
	write(t, bPath, "B\n@CLAUDE.md\n")

	blocks, err := memory.Load("", wd) // must return, not hang
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	count := 0
	for _, b := range blocks {
		if src, ok := memory.Source(b); ok && (src == aPath || src == bPath) {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("loaded %d cycle blocks, want each of A,B once", count)
	}
}

// TestImportDepthBounded is AS-082 AC2: a deep import chain stops at
// MaxImportDepth rather than following indefinitely.
func TestImportDepthBounded(t *testing.T) {
	wd := t.TempDir()
	// Build a chain f0 -> f1 -> ... longer than the depth limit.
	total := memory.MaxImportDepth + 3
	for i := 0; i < total; i++ {
		name := filepath.Join(wd, fmt.Sprintf("f%d.md", i))
		body := fmt.Sprintf("level %d", i)
		if i < total-1 {
			body += fmt.Sprintf("\n@f%d.md\n", i+1)
		}
		write(t, name, body)
	}
	// Load f0 directly by naming it CLAUDE.md content via a thin entry file.
	entry := filepath.Join(wd, "CLAUDE.md")
	write(t, entry, "@f0.md\n")

	blocks, err := memory.Load("", wd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	deepest := filepath.Join(wd, fmt.Sprintf("f%d.md", total-1))
	if _, ok := blockFor(blocks, deepest); ok {
		t.Fatalf("deepest file %s loaded past depth limit %d", deepest, memory.MaxImportDepth)
	}
}

// TestImportHomeExpansion checks that an @~/path import resolves against the
// user home dir (AS-082), not the filesystem root.
func TestImportHomeExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	importPath := filepath.Join(home, "global.md")
	write(t, importPath, "home rule")

	wd := t.TempDir()
	write(t, filepath.Join(wd, "CLAUDE.md"), "@~/global.md\n")

	blocks, err := memory.Load("", wd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	imp, ok := blockFor(blocks, importPath)
	if !ok {
		t.Fatalf("no block for home-expanded import %s (loaded %d)", importPath, len(blocks))
	}
	if imp.Text == nil || imp.Text.Text != "home rule" {
		t.Fatalf("content = %+v, want %q", imp.Text, "home rule")
	}
}

// TestImportMissingNote is AS-082 AC3: a missing import surfaces a visible note
// attributed to the target path and does not abort the load.
func TestImportMissingNote(t *testing.T) {
	wd := t.TempDir()
	mainPath := filepath.Join(wd, "CLAUDE.md")
	write(t, mainPath, "rule\n@nope.md\n")
	missing := filepath.Join(wd, "nope.md")

	blocks, err := memory.Load("", wd)
	if err != nil {
		t.Fatalf("load must not abort on missing import: %v", err)
	}
	note, ok := blockFor(blocks, missing)
	if !ok {
		t.Fatalf("no note block for missing import %s", missing)
	}
	if note.Text == nil || !strings.Contains(note.Text.Text, "not found") {
		t.Fatalf("note content = %+v, want a 'not found' note", note.Text)
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
