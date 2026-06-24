package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// codingModePackages are the Coding Mode subsystem packages whose reflect phase
// produces artifacts only: a success metric, an instrumentation diff, and a
// check-back ticket draft (AS-076). Judging whether a shipped feature succeeded
// needs the deployed app's runtime telemetry, which a coding harness must not try
// to read (coding-mode.prd.md D-CODE-7) — these packages deal in strings, not
// runtime data.
var codingModePackages = []string{
	"github.com/tonitienda/agent-smith/internal/mode",
	"github.com/tonitienda/agent-smith/internal/codingskills",
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
// telemetry-ingestion path (AS-076 AC2, D-CODE-7): nothing in the *transitive*
// dependency graph of its packages opens a network connection or a database, so
// the reflect phase cannot read or claim a shipped feature's runtime data. It
// produces artifacts only.
//
// The whole dependency closure is checked via `go list -deps` rather than just
// the direct imports, so a telemetry path cannot hide one hop away behind a
// first-party package. Third-party HTTP clients and database drivers are already
// barred from these core packages by TestCoreStaysStdlibFirst (PRD D6), so the
// forbidden set here is the stdlib network/database surface.
func TestCodingModeHasNoTelemetryIngestion(t *testing.T) {
	root := moduleRoot(t)
	for _, pkg := range codingModePackages {
		deps := transitiveDeps(t, root, pkg)
		for _, dep := range deps {
			if isTelemetryIngestion(dep) {
				t.Errorf("%s depends (transitively) on %q; the Coding Mode subsystem must have no telemetry-ingestion path — the reflect phase produces artifacts, it never reads shipped-app runtime data (AS-076, D-CODE-7).", pkg, dep)
			}
		}
	}
}

// transitiveDeps returns the full transitive import closure of pkg (its own
// imports plus everything they pull in), via the go tool so the graph is exact
// rather than re-derived by hand. It runs offline — `go list` resolves from the
// module cache, making no network call.
func transitiveDeps(t *testing.T, root, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
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
