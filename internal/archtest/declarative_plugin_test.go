package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeclarativePluginBoundaryHasNoExecOrEgress is the structural half of the D9
// declarative-only plugin boundary guard (AS-112, plugin-trust.md §4.3). A
// third-party sub-agent ships as a manifest (data), parsed by internal/subagent's
// ParseManifest/LoadManifest and wrapped in a passive `declarative` sub-agent. For
// "data, never code" to hold, the package that parses and drives that manifest must
// have no edge to arbitrary execution (os/exec) or network egress (net/http): there
// must be no way for a parsed third-party manifest to grow a code-execution or
// exfiltration path under a future refactor.
//
// We assert the property on internal/subagent itself: the parse → wrap → drive path
// lives entirely in that package, so if the package imports neither os/exec nor
// net/http, no such edge can exist on the declarative path. The behavioral guard
// (TestDeclarativeBoundaryNoOp in internal/subagent) covers the runtime half.
func TestDeclarativePluginBoundaryHasNoExecOrEgress(t *testing.T) {
	root := moduleRoot(t)
	const pkgDir = "internal/subagent"
	forbidden := map[string]string{
		"os/exec":  "the declarative plugin path must not reach arbitrary command execution",
		"net/http": "the declarative plugin path must not reach network egress",
	}

	dir := filepath.Join(root, filepath.FromSlash(pkgDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", pkgDir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		// Non-test sources only: a test may import os/exec to build fixtures; the
		// production boundary is what must stay clean.
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if reason, bad := forbidden[p]; bad {
				t.Errorf("%s/%s imports %q: %s (AS-112, D9). See docs/design/plugin-trust.md §4.3.", pkgDir, name, p, reason)
			}
		}
	}
}
