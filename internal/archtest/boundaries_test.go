// Package archtest holds module-wide architecture guard tests. It has no
// production code; it exists only to enforce structural contracts that no single
// package can check about itself.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modulePath is this module's import path. Imports under it are first-party.
const modulePath = "github.com/tonitienda/agent-smith"

// thirdPartyAllowed lists the module-relative package directory prefixes that
// may import third-party (non-stdlib, non-first-party) dependencies: the
// terminal face and the process composition roots under cmd/. Everything else is
// the stdlib-first core (see docs/architecture/dependency-boundaries.md).
//
// Adding an entry here is the documented escape hatch for a justified exception
// (PRD D6); keep this list and the boundaries doc in lockstep.
var thirdPartyAllowed = []string{
	"internal/tui",        // the interactive TUI face (Bubble Tea / Lip Gloss / Glamour)
	"internal/credential", // OS-keychain secret-store adapter (go-keyring); AS-017, D9
	"cmd",                 // executable composition roots wire faces and terminal setup
}

// TestCoreStaysStdlibFirst walks every non-test Go source in the module and
// fails if a core package imports a third-party dependency. Core packages must
// depend only on the Go standard library and this module so the architectural
// core remains portable and offline-testable (AS-095, PRD D6).
func TestCoreStaysStdlibFirst(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", ".git":
				return fs.SkipDir
			}
			// Skip whole subtrees that are allowed to import third-party code, so
			// the guard never parses the face or executable layers at all.
			if isThirdPartyAllowed(filepath.ToSlash(rel)) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		// Non-test sources only: a test file may import third-party packages to
		// build fixtures, matching the existing face-boundary guards.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if isThirdParty(p) {
				t.Errorf("%s imports third-party %q; core packages must stay stdlib-first (AS-095, PRD D6). If this is a justified exception, add the package to thirdPartyAllowed and docs/architecture/dependency-boundaries.md.", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

// isThirdPartyAllowed reports whether a module-relative package directory is in
// (or under) a layer permitted to import third-party dependencies.
func isThirdPartyAllowed(dir string) bool {
	for _, prefix := range thirdPartyAllowed {
		if dir == prefix || strings.HasPrefix(dir, prefix+"/") {
			return true
		}
	}
	return false
}

// isThirdParty reports whether an import path is neither standard library nor
// first-party. Standard-library paths have no dot in their first segment (e.g.
// "io/fs"); first-party paths live under this module.
func isThirdParty(importPath string) bool {
	if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
		return false
	}
	first, _, _ := strings.Cut(importPath, "/")
	return strings.Contains(first, ".")
}

// moduleRoot walks up from the test's working directory to the directory holding
// go.mod so the guard scans the whole module regardless of where `go test` runs.
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
