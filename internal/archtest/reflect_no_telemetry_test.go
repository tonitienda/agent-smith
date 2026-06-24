package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// codingModePackages are the Coding Mode subsystem package directories whose
// reflect phase produces artifacts only: a success metric, an instrumentation
// diff, and a check-back ticket draft (AS-076). Judging whether a shipped feature
// succeeded needs the deployed app's runtime telemetry, which a coding harness
// must not try to read (coding-mode.prd.md D-CODE-7) — these packages deal in
// strings, not runtime data.
var codingModePackages = []string{
	"internal/mode",
	"internal/codingskills",
}

// telemetryIngestionImports are the stdlib import prefixes that would let the
// Coding Mode subsystem reach out and ingest shipped-app runtime data — a network
// client or a database. The reflect phase has no business opening either: it
// scaffolds instrumentation for the user to run, it never collects the results.
var telemetryIngestionImports = []string{
	"net",          // covers net and net/http: any network client/socket
	"database/sql", // a metrics/analytics store
}

// TestCodingModeHasNoTelemetryIngestion asserts the Coding Mode subsystem has no
// telemetry-ingestion path (AS-076 AC2, D-CODE-7): its packages may not import a
// network client or a database, so the reflect phase cannot read or claim a
// shipped feature's runtime data. It produces artifacts only.
func TestCodingModeHasNoTelemetryIngestion(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	for _, pkg := range codingModePackages {
		pkgDir := filepath.Join(root, filepath.FromSlash(pkg))
		err := filepath.WalkDir(pkgDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			// Non-test sources only: a test may build fixtures with whatever it needs.
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return parseErr
			}
			rel, _ := filepath.Rel(root, path)
			for _, imp := range file.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				if isTelemetryIngestion(p) {
					t.Errorf("%s imports %q; the Coding Mode subsystem must have no telemetry-ingestion path — the reflect phase produces artifacts, it never reads shipped-app runtime data (AS-076, D-CODE-7).", rel, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", pkg, err)
		}
	}
}

// isTelemetryIngestion reports whether an import path is (or is under) a stdlib
// package that opens a network connection or a database.
func isTelemetryIngestion(importPath string) bool {
	for _, prefix := range telemetryIngestionImports {
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	return false
}
