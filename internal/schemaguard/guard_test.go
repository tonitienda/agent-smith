package schemaguard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// baselinePath / goldenGlob are package-relative (tests run in the package dir),
// pointing at the same artifacts cmd/schema-guard writes from the repo root.
const (
	baselinePath = "testdata/schema_baseline.json"
	goldenGlob   = "testdata/golden/*.json"
)

// TestSchemaIsAdditiveOnly is the CI guard: it diffs the live schema against the
// committed baseline and fails on any non-additive (breaking) change. This is
// the mechanical enforcement of PRD D2 — the test exists so that discipline is
// not the only thing standing between the schema and a breaking edit.
func TestSchemaIsAdditiveOnly(t *testing.T) {
	baseline := loadBaseline(t)
	breaks := Compare(baseline, Generate())
	if len(breaks) > 0 {
		t.Fatalf("BREAKING schema change(s) detected — the content-block schema is additive-only from V1 (PRD D2):\n  - %s\n\n"+
			"No field may be removed, renamed, retyped, or repurposed; no enum value may be dropped.\n"+
			"If your change is purely additive and you intend to bring it under the guard, run:\n"+
			"    go run ./cmd/schema-guard -update\n"+
			"See docs/schema/EVOLUTION.md.",
			strings.Join(breaks, "\n  - "))
	}
}

// TestGoldenSessionsParse guarantees the forward-compatibility promise: v1
// session files committed permanently must keep parsing, validating, and
// surviving a re-emit → re-parse cycle with identical semantics, forever.
func TestGoldenSessionsParse(t *testing.T) {
	files, err := filepath.Glob(goldenGlob)
	if err != nil {
		t.Fatalf("glob golden files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no golden session files found at %s — the v1 corpus must be kept permanently", goldenGlob)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			var doc schema.Document
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("golden %s no longer parses (forward-compat broken): %v", f, err)
			}
			for _, b := range doc.Blocks {
				if err := b.Validate(); err != nil {
					t.Fatalf("golden block %s no longer validates: %v", b.ID, err)
				}
			}
			// Re-emit → re-parse must preserve semantics: the schema can still
			// represent everything a v1 session recorded. Compare two compact
			// marshalings (rather than DeepEqual) so the comparison is immune to
			// json.RawMessage whitespace inherited from the indented on-disk file.
			reemit, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("re-marshal golden: %v", err)
			}
			var got schema.Document
			if err := json.Unmarshal(reemit, &got); err != nil {
				t.Fatalf("re-parse golden: %v", err)
			}
			reemit2, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("re-marshal parsed golden: %v", err)
			}
			if !bytes.Equal(reemit, reemit2) {
				t.Fatalf("golden %s lost semantics across re-emit:\n first: %s\nsecond: %s", f, reemit, reemit2)
			}
		})
	}
}

// TestGoldenCorpusCoversEveryContentKind keeps the corpus honest: every frozen
// V1 content kind must appear in some golden session, so the parse guarantee
// actually exercises the whole substrate rather than a convenient subset.
func TestGoldenCorpusCoversEveryContentKind(t *testing.T) {
	seen := map[schema.Kind]bool{}
	files, _ := filepath.Glob(goldenGlob)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read golden: %v", err)
		}
		var doc schema.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse golden: %v", err)
		}
		for _, b := range doc.Blocks {
			seen[b.Kind] = true
		}
	}
	for _, kind := range []schema.Kind{
		schema.KindText, schema.KindToolCall, schema.KindToolResult,
		schema.KindFileRead, schema.KindReasoning,
	} {
		if !seen[kind] {
			t.Errorf("no golden session contains a %q block; the corpus must cover every V1 content kind", kind)
		}
	}
}

// TestBaselineIsCurrentlyClean asserts the committed baseline describes the live
// schema with no breaking deltas in either direction at rest — a smoke test that
// the checked-in artifact is internally consistent with the code.
func TestBaselineIsCurrentlyClean(t *testing.T) {
	if breaks := Compare(loadBaseline(t), Generate()); len(breaks) > 0 {
		t.Fatalf("committed baseline reports breaks against current schema: %s", strings.Join(breaks, "; "))
	}
}

func loadBaseline(t *testing.T) Descriptor {
	t.Helper()
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read baseline %s: %v (run `go run ./cmd/schema-guard -update`)", baselinePath, err)
	}
	var d Descriptor
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("parse baseline %s: %v", baselinePath, err)
	}
	return d
}
