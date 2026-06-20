package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLayeringContracts asserts the dependency *direction* between core layers
// (AS-098). Where the stdlib-first guard (boundaries_test.go) limits what a
// package may import from outside the module, these contracts limit how core
// packages may depend on one another so orchestration, faces, and adapters stay
// layered: dependencies point inward toward contracts and leaves, never back out
// toward the loop, faces, or composition roots.
//
// See docs/architecture/package-contracts.md for the prose rules; this test is
// the enforcement so the documentation cannot silently drift.
func TestLayeringContracts(t *testing.T) {
	root := moduleRoot(t)

	cases := []struct {
		name string
		// pkgDir is the module-relative directory of the package under test
		// (non-recursive: only its own .go files are inspected).
		pkgDir string
		// forbidden lists module-relative directory prefixes the package must
		// not import, directly or as a subpackage.
		forbidden []string
		reason    string
	}{
		{
			name:      "provider contracts do not import concrete providers",
			pkgDir:    "internal/provider",
			forbidden: []string{"internal/provider/anthropic", "internal/provider/openai"},
			reason:    "the provider contract must not depend on its concrete adapters; the dependency points inward, from each adapter to the interface",
		},
		{
			name:      "anthropic adapter does not import loop, faces, or composition roots",
			pkgDir:    "internal/provider/anthropic",
			forbidden: []string{"internal/loop", "internal/tui", "cmd"},
			reason:    "concrete providers are leaves: the loop, faces, and cmd/* wire them up, never the reverse",
		},
		{
			name:      "openai adapter does not import loop, faces, or composition roots",
			pkgDir:    "internal/provider/openai",
			forbidden: []string{"internal/loop", "internal/tui", "cmd"},
			reason:    "concrete providers are leaves: the loop, faces, and cmd/* wire them up, never the reverse",
		},
		{
			name:      "loop does not import face packages",
			pkgDir:    "internal/loop",
			forbidden: []string{"internal/tui"},
			reason:    "the agent loop is face-agnostic; faces drive the loop, not the reverse",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			imports := packageImports(t, root, tc.pkgDir)
			for _, imp := range imports {
				for _, bad := range tc.forbidden {
					full := modulePath + "/" + bad
					if imp == full || strings.HasPrefix(imp, full+"/") {
						t.Errorf("%s imports %q, violating an architecture contract: %s (AS-098). See docs/architecture/package-contracts.md.", tc.pkgDir, imp, tc.reason)
					}
				}
			}
		})
	}
}

// packageImports returns every import path used by the non-test Go sources
// directly inside a module-relative package directory. It does not recurse into
// subpackages, so a contract about one package is not confused by a sibling.
func packageImports(t *testing.T, root, relDir string) []string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", relDir, err)
	}

	fset := token.NewFileSet()
	var imports []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			imports = append(imports, strings.Trim(imp.Path.Value, `"`))
		}
	}
	return imports
}
