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
		// not import, directly or as a subpackage. "faces" means both face
		// packages: internal/tui and internal/serve.
		forbidden []string
		// forbidModule imports means the package must not import any package from
		// this module. Use it for stdlib-only leaf packages.
		forbidModule bool
		reason       string
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
			forbidden: []string{"internal/loop", "internal/tui", "internal/serve", "cmd"},
			reason:    "concrete providers are leaves: the loop, faces, and cmd/* wire them up, never the reverse",
		},
		{
			name:      "openai adapter does not import loop, faces, or composition roots",
			pkgDir:    "internal/provider/openai",
			forbidden: []string{"internal/loop", "internal/tui", "internal/serve", "cmd"},
			reason:    "concrete providers are leaves: the loop, faces, and cmd/* wire them up, never the reverse",
		},
		{
			name:         "render primitives do not import module packages",
			pkgDir:       "internal/render",
			forbidModule: true,
			reason:       "render primitives are a stdlib-only leaf shared by feature renderers",
		},
		{
			name:         "streamio does not import module packages",
			pkgDir:       "internal/streamio",
			forbidModule: true,
			reason:       "stream I/O primitives are a stdlib-only leaf shared by adapters and transports",
		},
		{
			name:      "loop does not import face packages",
			pkgDir:    "internal/loop",
			forbidden: []string{"internal/tui", "internal/serve"},
			reason:    "the agent loop is face-agnostic; faces drive the loop, not the reverse",
		},
		{
			name:      "event log does not import projection, provider, loop, or faces",
			pkgDir:    "internal/eventlog",
			forbidden: []string{"internal/projection", "internal/provider", "internal/loop", "internal/tui", "internal/serve"},
			reason:    "the event log is a lower-level storage layer; higher layers read it, not the reverse",
		},
		{
			name:      "projection does not import provider, loop, or faces",
			pkgDir:    "internal/projection",
			forbidden: []string{"internal/provider", "internal/loop", "internal/tui", "internal/serve"},
			reason:    "projections are built over the event log and must not depend on orchestration or faces",
		},
		{
			name:      "tools do not import loop or faces",
			pkgDir:    "internal/tool",
			forbidden: []string{"internal/loop", "internal/tui", "internal/serve"},
			reason:    "tools are leaves the loop wires in; they must not reach back into the loop or faces",
		},
		{
			name:      "builtin tools do not import loop or faces",
			pkgDir:    "internal/tool/builtin",
			forbidden: []string{"internal/loop", "internal/tui", "internal/serve"},
			reason:    "the shipped tools stay leaves; the task tool (AS-046) depends on the builtin.Spawner seam, not the loop, so the delegation wiring lives in the orchestration layer (internal/delegate)",
		},
		{
			name:      "delegate does not import faces",
			pkgDir:    "internal/delegate",
			forbidden: []string{"internal/tui", "internal/serve"},
			reason:    "delegate is orchestration: it may use the loop, providers, tools, and session store but never a face (AS-046)",
		},
		{
			name:      "manifest does not import provider, loop, faces, or composition roots",
			pkgDir:    "internal/manifest",
			forbidden: []string{"internal/provider", "internal/loop", "internal/tui", "internal/serve", "cmd"},
			reason:    "the run manifest (AS-055) is a derived view over the log and cost accounting; it must not depend on orchestration, providers, or faces",
		},
		{
			name:      "otelexport does not import provider, loop, projection, faces, or composition roots",
			pkgDir:    "internal/otelexport",
			forbidden: []string{"internal/provider", "internal/loop", "internal/projection", "internal/tui", "internal/serve", "cmd"},
			reason:    "the OpenTelemetry exporter (AS-055) projects the log + cost into a trace; it must not depend on orchestration, providers, or faces",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			imports := packageImports(t, root, tc.pkgDir)
			for _, imp := range imports {
				if tc.forbidModule && isModuleImport(imp) {
					t.Errorf("%s imports %q, violating an architecture contract: %s (AS-098). See docs/architecture/package-contracts.md.", tc.pkgDir, imp, tc.reason)
				}
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

func isModuleImport(imp string) bool {
	return imp == modulePath || strings.HasPrefix(imp, modulePath+"/")
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
	seen := make(map[string]bool)
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
			seen[strings.Trim(imp.Path.Value, `"`)] = true
		}
	}

	// Deduplicate so a single violation imported by several files in the package
	// is reported once, not once per file.
	imports := make([]string, 0, len(seen))
	for imp := range seen {
		imports = append(imports, imp)
	}
	return imports
}
