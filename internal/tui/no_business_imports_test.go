package tui

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestNoProviderOrToolImports enforces the AS-021 boundary: the TUI is a thin
// face over the loop's UIEvent stream and must import neither internal/provider
// nor internal/tool. It parses the package's non-test sources and fails on any
// such import. (Test files may import them to build fixtures.)
func TestNoProviderOrToolImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	forbidden := []string{"internal/provider", "internal/tool"}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if strings.Contains(p, bad) {
					t.Errorf("%s imports %q; the TUI must stay face-agnostic (AS-021)", name, p)
				}
			}
		}
	}
}
