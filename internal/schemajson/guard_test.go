package schemajson_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/schemaguard"
	"github.com/tonitienda/agent-smith/internal/schemajson"
)

// schemaPath is the published JSON Schema, relative to this package directory.
const schemaPath = "../../docs/schema/block.schema.json"

// goldenDir holds the permanently-kept v1 session corpus the additive-only
// guard (AS-004) maintains; the JSON-Schema guard reuses it as its valid corpus.
const goldenDir = "../schemaguard/testdata/golden"

func compile(t *testing.T) *schemajson.Schema {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	s, err := schemajson.Compile(data)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return s
}

// TestGoMarshaledDocumentsValidate ties the published JSON Schema to the live Go
// reference types: every document the Go schema package marshals (including the
// maximal full-coverage session) must validate. A Go change that emits a shape
// the schema rejects fails here — the divergence guard (AS-061 criterion 5).
func TestGoMarshaledDocumentsValidate(t *testing.T) {
	s := compile(t)
	for name, doc := range schemaguard.GeneratedGoldens() {
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := s.Validate(data); err != nil {
			t.Errorf("Go-marshaled %s should validate:\n%v", name, err)
		}
	}
}

// TestCommittedGoldenCorpusValidates validates the on-disk v1 golden corpus,
// including the hand-written forward-compat fixture (unknown top-level field,
// unknown envelope/body fields, and an unknown block kind).
func TestCommittedGoldenCorpusValidates(t *testing.T) {
	s := compile(t)
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(goldenDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := s.Validate(data); err != nil {
			t.Errorf("golden %s should validate:\n%v", e.Name(), err)
		}
	}
}

// TestForwardCompatValidates proves the schema honors additive evolution
// (PRD D2): a document carrying unknown fields and an unknown block kind still
// validates (AS-061 criterion 3).
func TestForwardCompatValidates(t *testing.T) {
	s := compile(t)
	doc := `{
		"schema": "agent-smith.blocks.v1",
		"schema_version": "1",
		"surprise_top_level": {"x": 1},
		"blocks": [
			{
				"id": "blk_1", "kind": "text", "seq": 0,
				"ts": "2026-06-23T00:00:00Z", "role": "assistant",
				"future_envelope_field": [1, 2, 3],
				"text": {"text": "hi", "future_body_field": true}
			},
			{
				"id": "blk_2", "kind": "telemetry", "seq": 1,
				"ts": "2026-06-23T00:00:01Z", "role": "harness",
				"telemetry": {"latency_ms": 42}
			}
		]
	}`
	if err := s.Validate([]byte(doc)); err != nil {
		t.Errorf("forward-compat document should validate:\n%v", err)
	}
}

// TestInvalidCorpusIsRejected proves the schema actually constrains: each
// document violates a forever-invariant and must be rejected (AS-061
// criterion 4). The label names the invariant under test.
func TestInvalidCorpusIsRejected(t *testing.T) {
	s := compile(t)
	cases := map[string]string{
		"missing required envelope id": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"kind": "text", "seq": 0, "ts": "t", "role": "user", "text": {}}]
		}`,
		"body does not match kind": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"id": "b", "kind": "tool_call", "seq": 0, "ts": "t", "role": "assistant", "text": {"text": "oops"}}]
		}`,
		"tool_call missing tool_use_id and name": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"id": "b", "kind": "tool_call", "seq": 0, "ts": "t", "role": "assistant", "tool_call": {"arguments": {}}}]
		}`,
		"tool_result missing tool_use_id": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"id": "b", "kind": "tool_result", "seq": 0, "ts": "t", "role": "tool", "tool_result": {"stdout": "x"}}]
		}`,
		"file_read missing path": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"id": "b", "kind": "file_read", "seq": 0, "ts": "t", "role": "harness", "file_read": {"content": "x"}}]
		}`,
		"seq has wrong type": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"id": "b", "kind": "text", "seq": "one", "ts": "t", "role": "user", "text": {"text": "hi"}}]
		}`,
		"negative token count": `{
			"schema": "x", "schema_version": "1",
			"blocks": [{"id": "b", "kind": "text", "seq": 0, "ts": "t", "role": "assistant", "text": {"text": "hi"}, "tokens": {"input": -5}}]
		}`,
		"blocks is not an array": `{
			"schema": "x", "schema_version": "1", "blocks": {"not": "an array"}
		}`,
	}
	for label, doc := range cases {
		if err := s.Validate([]byte(doc)); err == nil {
			t.Errorf("%s: expected rejection, got none", label)
		}
	}
}
