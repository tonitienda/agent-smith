package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// orchestrationAndFacePackages lists the module-relative directory prefixes of
// the orchestration / consumer / face / composition-root layer — the packages
// that sit *at or above* the loop and may freely depend on the inward core. The
// blanket invariant guarded by TestInwardCoreDoesNotImportOrchestration (AS-146)
// is the most load-bearing rule in docs/architecture/package-contracts.md:
// "dependencies point inward", so nothing in the inward core may import any of
// these.
//
// This is the lowest-maintenance form of the guard (AS-146 option b): a new
// inward-core package is covered automatically (it just must not import anything
// here). The cost is that a *new* orchestration/face package must be appended
// below, or the guard will treat it as inward and fail when it imports a sibling
// orchestration package — that failure is the reminder to update the list.
//
// Membership rationale (see package-contracts.md "Dependency direction"):
//   - loop, benchmark, delegate — orchestration that drives the loop.
//   - insights, insightsmodel, stats, statsindex, improve, skillrollup — the
//     surfacing/analytics consumers that sit at the face layer (nothing inward
//     may import them); the intra-orchestration edges they have between
//     themselves (insightsmodel→insights, statsindex→stats, improve→skillrollup,
//     stats→skillrollup) are allowed precisely because both ends live here.
//   - tui, serve — the faces.
//   - smithapp, cmd, e2e — composition roots and the end-to-end harness, which
//     legitimately wire everything together.
var orchestrationAndFacePackages = []string{
	"internal/loop",
	"internal/orchestrator",
	"internal/benchmark",
	"internal/delegate",
	"internal/insights",
	"internal/insightsmodel",
	"internal/stats",
	"internal/statsindex",
	"internal/improve",
	"internal/skillrollup",
	"internal/tui",
	"internal/serve",
	"internal/smithapp",
	"internal/e2e",
	"cmd",
}

// TestInwardCoreDoesNotImportOrchestration asserts the blanket inward-pointing
// rule: every first-party package that is *not* in the orchestration/face/root
// layer must not import any package that is. Where layering_test.go enforces the
// rule per-orchestration-package and for individual inward leaves, this guard
// closes the open-ended case — any current or future inward package reaching
// "up" to the loop, a face, an analytics consumer, or a composition root (AS-146).
func TestInwardCoreDoesNotImportOrchestration(t *testing.T) {
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
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", ".git":
				return fs.SkipDir
			}
			// The orchestration/face/root layer is allowed to import inward
			// packages, so skip those subtrees entirely — they are not "inward".
			if inOrchestrationLayer(relSlash) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		// Non-test sources only, matching the other architecture guards: a test
		// file may import an orchestration package to build fixtures.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		pkgDir := filepath.ToSlash(filepath.Dir(rel))
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if bad := orchestrationImport(p); bad != "" {
				t.Errorf("inward-core package %s imports orchestration/face package %q, violating the inward-dependency rule: dependencies point inward, never up toward the loop, faces, analytics consumers, or composition roots (AS-146). See docs/architecture/package-contracts.md. If %s is itself an orchestration/face package, add it to orchestrationAndFacePackages.", pkgDir, p, pkgDir)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

// inOrchestrationLayer reports whether a module-relative directory is in (or
// under) an orchestration/face/composition-root package.
func inOrchestrationLayer(dir string) bool {
	for _, prefix := range orchestrationAndFacePackages {
		if dir == prefix || strings.HasPrefix(dir, prefix+"/") {
			return true
		}
	}
	return false
}

// orchestrationImport returns the offending module-relative package prefix if
// the import path points into the orchestration/face/root layer, or "" if it is
// allowed (stdlib, third-party, or an inward first-party package).
func orchestrationImport(importPath string) string {
	if !isModuleImport(importPath) {
		return ""
	}
	rel := strings.TrimPrefix(importPath, modulePath+"/")
	if inOrchestrationLayer(rel) {
		return importPath
	}
	return ""
}
