package personality

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// personalityImportPath is this package's import path; the substance-producing
// packages below must never reach it.
const personalityImportPath = "github.com/tonitienda/agent-smith/internal/personality"

// forbiddenDirs are the module-relative package directories that produce
// substance — generated code, diffs, commits, file writes, error payloads, and
// programmatic output — and so must have no import path to the flavor package
// (AS-053 AC2, PRD §7.21). The containment guarantee is enforced here, not by
// review: flavor lives only in chrome, never in any of these paths.
var forbiddenDirs = []string{
	"internal/loop",       // the orchestrator and its UIEvents
	"internal/provider",   // model request/response payloads
	"internal/tool",       // file writes, shell, edits/diffs
	"internal/cost",       // accounting output
	"internal/eventlog",   // the persisted block log (file writes)
	"internal/projection", // model-facing context assembly
	"internal/compact",    // summarized log content
	"internal/clean",      // log mutation/output
	"schema",              // the wire/log block schema
}

func TestForbiddenPackagesDoNotImportPersonality(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	for _, dir := range forbiddenDirs {
		base := filepath.Join(root, filepath.FromSlash(dir))
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("forbidden dir %q not found (rename or move? keep this guard honest): %v", dir, err)
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			// Non-test sources only: a test file may import the package to build a
			// fixture, just as the TUI's own boundary test allows.
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				if strings.Trim(imp.Path.Value, `"`) == personalityImportPath {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s imports the personality (flavor) package; %s must stay substance-only (AS-053)", rel, dir)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// moduleRoot walks up from the test's working directory to the directory holding
// go.mod, so the guard scans the whole module regardless of where `go test` runs.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root (go.mod) not found above the test directory")
		}
		dir = parent
	}
}
