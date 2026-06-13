package schema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }

var fixedTS = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// envelope returns a fully-populated envelope (every optional set) for the
// given kind so round-trip tests exercise the whole shared shape.
func envelope(id string, kind Kind) Block {
	return Block{
		ID:         id,
		Kind:       kind,
		Seq:        7,
		TS:         fixedTS,
		Role:       RoleAssistant,
		StopReason: "end_turn",
		Provenance: &Provenance{
			Producer:    "anthropic-adapter",
			RequestID:   "req_1",
			ResponseID:  "resp_1",
			TurnID:      "turn_1",
			DerivedFrom: []string{"blk_a", "blk_b"},
			Ext:         map[string]json.RawMessage{"prov_x": json.RawMessage(`"y"`)},
		},
		Provider: &Provider{
			Vendor:     "anthropic",
			Surface:    "messages",
			Model:      "claude-opus-4-8",
			NativeType: "text",
			NativeID:   "msg_123",
			Ext:        map[string]json.RawMessage{"raw": json.RawMessage(`{"k":1}`)},
		},
		Thread: &Thread{
			ThreadID:       "t_1",
			ParentBlockID:  "blk_parent",
			ParentThreadID: "t_root",
			AgentID:        "agent_7",
			IsSidechain:    true,
			Ext:            map[string]json.RawMessage{"th": json.RawMessage(`true`)},
		},
		Attribution: &Attribution{
			Skill:     "deep-research",
			MCPServer: "github",
			MCPTool:   "list_issues",
			Tool:      "Grep",
			Hook:      "PostToolUse",
			Ext:       map[string]json.RawMessage{"a": json.RawMessage(`1`)},
		},
		Tokens: &Tokens{
			Input:        intPtr(100),
			Output:       intPtr(0), // zero is meaningful and must round-trip as present
			CacheRead:    intPtr(20),
			CacheWrite:   intPtr(30),
			Reasoning:    intPtr(5),
			CacheWrite5m: intPtr(11),
			CacheWrite1h: intPtr(22),
			Iterations:   []Tokens{{Input: intPtr(50), Output: intPtr(60)}},
			Ext:          map[string]json.RawMessage{"tk": json.RawMessage(`"v"`)},
		},
		CostUSD:   floatPtr(0.0123),
		UsageMeta: &UsageMeta{ServiceTier: "standard", Speed: "fast", ServerToolUse: json.RawMessage(`{"web_search_requests":2}`)},
		Cache: &Cache{
			Mode:        CacheModeExplicit,
			Breakpoints: []CacheBreakpoint{{BlockID: "blk_bp", TTL: "5m"}},
			TTL:         "5m",
		},
		ExcludedBy: []string{"evt_1"},
		Ext:        map[string]json.RawMessage{"env_x": json.RawMessage(`{"nested":[1,2,3]}`)},
	}
}

// sampleBlocks returns one fully-populated block per frozen V1 content kind.
func sampleBlocks() []Block {
	text := envelope("blk_text", KindText)
	text.Text = &TextBody{
		Text:        "hello",
		Subtype:     TextSubtypeNormal,
		Parts:       []Part{{Type: "image", MediaType: "image/png", URL: "https://x/y.png"}},
		Citations:   []Citation{{Type: "web", URL: "https://src", Title: "Src", CitedText: "quote", Source: "live_search"}},
		Annotations: []json.RawMessage{json.RawMessage(`{"type":"url_citation"}`)},
		Ext:         map[string]json.RawMessage{"tb": json.RawMessage(`"z"`)},
	}

	call := envelope("blk_call", KindToolCall)
	call.Role = RoleAssistant
	call.ToolCall = &ToolCallBody{
		ToolUseID:     "toolu_1",
		Name:          "shell",
		Arguments:     json.RawMessage(`{"cmd":"ls"}`),
		ArgumentsRaw:  `{"cmd":"ls"}`,
		ToolKind:      ToolKindClient,
		ToolSubtype:   "",
		ParallelGroup: "grp_1",
		MCPServer:     "github",
		Ext:           map[string]json.RawMessage{"cb": json.RawMessage(`1`)},
	}

	result := envelope("blk_result", KindToolResult)
	result.Role = RoleTool
	result.ToolResult = &ToolResultBody{
		ToolUseID:         "toolu_1",
		Content:           []Part{{Type: "text", Text: "file listing"}},
		IsError:           true,
		Citations:         []Citation{{URL: "https://c"}},
		ExitCode:          intPtr(0), // zero exit code must round-trip as present
		Stdout:            "out",
		Stderr:            "err",
		StructuredContent: json.RawMessage(`{"files":["a","b"]}`),
		Truncated:         true,
		OffloadRef:        "blob://123",
		Ext:               map[string]json.RawMessage{"rb": json.RawMessage(`"r"`)},
	}

	fileRead := envelope("blk_fileread", KindFileRead)
	fileRead.Role = RoleHarness
	fileRead.FileRead = &FileReadBody{
		Path:        "/home/user/agent-smith/go.mod",
		Range:       &LineRange{StartLine: intPtr(1), EndLine: intPtr(3)},
		Content:     "module ...",
		ContentHash: "sha256:abc",
		OffloadRef:  "",
		Error:       "",
		ProducedBy:  "toolu_read",
		MediaType:   "text/plain",
		Source:      FileSourceTool,
		Ext:         map[string]json.RawMessage{"fb": json.RawMessage(`"f"`)},
	}

	reasoning := envelope("blk_reasoning", KindReasoning)
	reasoning.Reasoning = &ReasoningBody{
		Text:        "thinking...",
		Summary:     []string{"step 1", "step 2"},
		Encrypted:   "OPAQUE==",
		Signature:   "sig_abc",
		Redacted:    true,
		ReplayScope: ReplaySameModelOnly,
		Ext:         map[string]json.RawMessage{"rsb": json.RawMessage(`"q"`)},
	}

	return []Block{text, call, result, fileRead, reasoning}
}

func TestBlockRoundTripLossless(t *testing.T) {
	for _, want := range sampleBlocks() {
		t.Run(string(want.Kind), func(t *testing.T) {
			data, err := json.Marshal(want)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Block
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round-trip mismatch for %s\n want: %#v\n  got: %#v", want.Kind, want, got)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("validate after round-trip: %v", err)
			}
		})
	}
}

func TestDocumentRoundTripLossless(t *testing.T) {
	want := NewDocument(sampleBlocks()...)
	want.Ext = map[string]json.RawMessage{"doc_x": json.RawMessage(`1`)}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Document
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("document round-trip mismatch\n want: %#v\n  got: %#v", want, got)
	}
	if got.Schema != SchemaID || got.Version != SchemaVersion {
		t.Fatalf("schema tag lost: %q %q", got.Schema, got.Version)
	}
}

// TestNewDocumentMarshalsEmptyBlocks guards that a document built with no
// blocks serializes "blocks" as [] rather than null, for client robustness.
func TestNewDocumentMarshalsEmptyBlocks(t *testing.T) {
	data, err := json.Marshal(NewDocument())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"blocks":[]`) {
		t.Fatalf("empty document must marshal blocks as [], got %s", data)
	}
}

// TestUnknownFieldTolerance verifies forward compatibility (PRD D2): a document
// produced by a newer schema version — with unknown fields at the top level,
// inside a body, and inside sub-objects, plus an unknown block kind — must
// deserialize without error, preserving the fields we do understand.
func TestUnknownFieldTolerance(t *testing.T) {
	const doc = `{
	  "schema": "agent-smith.blocks.v1",
	  "schema_version": "1",
	  "future_top_level": {"anything": true},
	  "blocks": [
	    {
	      "id": "blk_1",
	      "kind": "text",
	      "seq": 0,
	      "ts": "2026-06-13T12:00:00Z",
	      "role": "assistant",
	      "future_envelope_field": 42,
	      "provider": {"vendor": "openai", "future_provider_field": "x"},
	      "text": {"text": "hi", "future_body_field": [1,2,3]}
	    },
	    {
	      "id": "blk_2",
	      "kind": "plan",
	      "seq": 1,
	      "ts": "2026-06-13T12:00:01Z",
	      "role": "assistant",
	      "plan": {"items": ["do a thing"]}
	    }
	  ]
	}`

	var got Document
	if err := json.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("unknown-field document must deserialize, got error: %v", err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].Text == nil || got.Blocks[0].Text.Text != "hi" {
		t.Fatalf("known field lost: %#v", got.Blocks[0].Text)
	}
	if got.Blocks[0].Provider == nil || got.Blocks[0].Provider.Vendor != "openai" {
		t.Fatalf("known sub-object field lost: %#v", got.Blocks[0].Provider)
	}
	// An unknown kind ("plan") with an unknown body must still validate
	// (tolerate-unknown), since we cannot know its body shape.
	if err := got.Blocks[1].Validate(); err != nil {
		t.Fatalf("unknown kind must validate, got: %v", err)
	}
}

func TestNewIDUniqueAndPrefixed(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := NewID()
		if !strings.HasPrefix(id, idPrefix) {
			t.Fatalf("id %q missing prefix %q", id, idPrefix)
		}
		if seen[id] {
			t.Fatalf("duplicate id generated: %q", id)
		}
		seen[id] = true
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		block   Block
		wantErr bool
	}{
		{
			name:  "valid text",
			block: Block{ID: "blk_1", Kind: KindText, Text: &TextBody{Text: "hi"}},
		},
		{
			name:    "missing id",
			block:   Block{Kind: KindText, Text: &TextBody{}},
			wantErr: true,
		},
		{
			name:    "missing kind",
			block:   Block{ID: "blk_1", Text: &TextBody{}},
			wantErr: true,
		},
		{
			name:    "content kind without body",
			block:   Block{ID: "blk_1", Kind: KindText},
			wantErr: true,
		},
		{
			name:    "content kind with wrong body",
			block:   Block{ID: "blk_1", Kind: KindText, Reasoning: &ReasoningBody{}},
			wantErr: true,
		},
		{
			name:    "content kind with two bodies",
			block:   Block{ID: "blk_1", Kind: KindText, Text: &TextBody{}, Reasoning: &ReasoningBody{}},
			wantErr: true,
		},
		{
			name:  "unknown kind tolerated with no body",
			block: Block{ID: "blk_1", Kind: Kind("plan")},
		},
		{
			name:  "derived compaction kind tolerated",
			block: Block{ID: "blk_1", Kind: KindCompaction, Provenance: &Provenance{DerivedFrom: []string{"blk_a"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.block.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestTokensZeroVsAbsent guards the "missing means unreported, never zero" rule
// (union §8): a reported zero must serialize as present, an unreported field
// must be omitted entirely.
func TestTokensZeroVsAbsent(t *testing.T) {
	tk := Tokens{Output: intPtr(0)} // Input unreported, Output reported as zero
	data, err := json.Marshal(tk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"output":0`) {
		t.Fatalf("reported zero must be present, got %s", s)
	}
	if strings.Contains(s, `"input"`) {
		t.Fatalf("unreported field must be omitted, got %s", s)
	}

	var got Tokens
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Input != nil {
		t.Fatalf("unreported input must stay nil, got %v", *got.Input)
	}
	if got.Output == nil || *got.Output != 0 {
		t.Fatalf("reported zero output must round-trip, got %v", got.Output)
	}
}
