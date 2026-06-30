package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docCompletenessAllowlist lists module-relative package directories that are
// intentionally *not* named in docs/architecture/package-contracts.md. The
// narrative map documents the runtime architecture; repo tooling under cmd/ that
// exists only to maintain the repo (sync tickets, capture fixtures, run the
// schema guard) is out of that scope and lives in its own docs instead.
//
// Adding an entry here is the documented escape hatch: a new package must either
// be named in package-contracts.md or appear here, on pain of a failing test.
// That is the same trade orchestrationAndFacePackages and thirdPartyAllowed make.
var docCompletenessAllowlist = []string{
	"cmd/capture-fixture", // AS-135 redacted-capture workflow tooling
	"cmd/schema-guard",    // AS-004 additive-only schema guard runner
	"cmd/ticket-sync",     // backlog↔issue sync tooling
}

// backtickToken matches a single “-quoted span in the doc, e.g. `internal/loop`
// or `goal`. The doc references packages as backticked tokens, so an exact match
// against the basename or full module-relative path is a far stronger signal than
// a bare substring (which would match "composition" inside the prose
// "composition root").
var backtickToken = regexp.MustCompile("`([^`]+)`")

// TestPackageContractsCompleteness asserts that the prose map in
// docs/architecture/package-contracts.md stays complete: every first-party
// package directory under internal/ and cmd/ that ships production code must be
// named in the doc (as a backticked basename or full module-relative path) or be
// on docCompletenessAllowlist (AS-162). It recurses, so nested packages
// (internal/provider/anthropic, internal/tool/builtin, internal/orchestrator/spec,
// …) are guarded too, not only the immediate children.
//
// The directional contracts in the doc are already enforced (layering_test.go,
// inward_core_test.go, boundaries_test.go), but the *completeness* of the map was
// not — a new package could be added with correct layering and never appear in
// the doc, which is exactly the drift a 2026-06-30 QA pass found. This guard
// closes that gap so the human-facing map cannot silently diverge from the code.
func TestPackageContractsCompleteness(t *testing.T) {
	root := moduleRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "package-contracts.md"))
	if err != nil {
		t.Fatalf("read package-contracts.md: %v", err)
	}
	documented := make(map[string]bool)
	for _, m := range backtickToken.FindAllStringSubmatch(string(data), -1) {
		documented[m[1]] = true
	}

	for _, parent := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, parent), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			switch {
			case d.Name() == "testdata", d.Name() == "vendor", strings.HasPrefix(d.Name(), "."):
				return fs.SkipDir
			}
			if !hasProductionGo(t, path) {
				// Test-only or intermediate directories (e.g. internal/archtest,
				// or a parent that only groups subpackages) carry no production
				// seam to document.
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			relDir := filepath.ToSlash(rel)
			if accountedFor(relDir, d.Name(), documented) {
				return nil
			}
			t.Errorf("package %s is not named in docs/architecture/package-contracts.md and is not on docCompletenessAllowlist (AS-162). Add a backticked mention (`%s` or `%s`) to the doc, or append it to docCompletenessAllowlist if it is repo tooling outside the architecture map.", relDir, d.Name(), relDir)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s/: %v", parent, err)
		}
	}
}

// accountedFor reports whether a package directory is documented (by basename or
// full module-relative path as a backticked token) or explicitly allowlisted.
func accountedFor(relDir, base string, documented map[string]bool) bool {
	for _, allowed := range docCompletenessAllowlist {
		if relDir == allowed {
			return true
		}
	}
	return documented[relDir] || documented[base]
}

// hasProductionGo reports whether a directory contains at least one non-test Go
// source file directly (not recursing), i.e. it ships production code worth
// documenting.
func hasProductionGo(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		return true
	}
	return false
}
