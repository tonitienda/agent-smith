package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FixtureSource classifies where a recorded fixture came from, so a reviewer can
// tell a redacted real capture — which may still expose unanticipated schema
// gaps (AS-060) — from a synthetic edge case, which by construction only contains
// fields we already anticipated.
type FixtureSource string

const (
	// SourceSynthetic is a hand-authored or curated fixture; no real account ever
	// touched it.
	SourceSynthetic FixtureSource = "synthetic"
	// SourceRedactedReal is a real vendor capture with secrets/PII stripped via the
	// AS-060/AS-135 capture-to-fixture workflow.
	SourceRedactedReal FixtureSource = "redacted-real"
)

// FixtureMeta records provenance for one conformance fixture.
type FixtureMeta struct {
	Source FixtureSource `json:"source"`
	Intent string        `json:"intent"`
}

// ManifestName is the per-directory file mapping each "<case>.http" fixture (by
// case name) to its FixtureMeta.
const ManifestName = "fixtures.json"

// LoadManifest reads the fixture manifest in dir.
func LoadManifest(dir string) (map[string]FixtureMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName)) //nolint:gosec // test-controlled fixture dir
	if err != nil {
		return nil, err
	}
	var m map[string]FixtureMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// AssertFixtureMetadata fails t unless every "<case>.http" fixture in dir has a
// manifest entry with a recognized source, and the manifest carries no entry for
// a missing fixture. This keeps real redacted captures distinguishable from
// synthetic edge cases as the corpus grows.
func AssertFixtureMetadata(t *testing.T, dir string) {
	t.Helper()
	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("loading fixture manifest: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading fixture dir: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".http") {
			continue
		}
		caseName := strings.TrimSuffix(name, ".http")
		seen[caseName] = true
		meta, ok := manifest[caseName]
		if !ok {
			t.Errorf("fixture %q has no entry in %s", name, ManifestName)
			continue
		}
		switch meta.Source {
		case SourceSynthetic, SourceRedactedReal:
		default:
			t.Errorf("fixture %q has unknown source %q (want %q or %q)",
				name, meta.Source, SourceSynthetic, SourceRedactedReal)
		}
	}
	for caseName := range manifest {
		if !seen[caseName] {
			t.Errorf("%s lists %q but no %s.http fixture exists", ManifestName, caseName, caseName)
		}
	}
}
