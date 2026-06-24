package capturefixture_test

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/capturefixture"
	"github.com/tonitienda/agent-smith/internal/redaction"
	"github.com/tonitienda/agent-smith/schema"
)

// rawCapture builds a small two-block session resembling a redacted L3 capture:
// a user turn whose text leaks a secret, and a sub-agent assistant turn that
// links back to the first block via thread parent and provenance.derived_from.
func rawCapture() []schema.Block {
	return []schema.Block{
		{
			ID:   "msg_real_0001",
			Kind: schema.KindText,
			Seq:  7,
			Role: schema.RoleUser,
			Provenance: &schema.Provenance{
				RequestID: "req_vendor_abc",
				TurnID:    "turn_xyz",
			},
			Provider: &schema.Provider{Vendor: "anthropic", Surface: "messages", NativeID: "msg_native_99"},
			Text:     &schema.TextBody{Text: "use key sk-ant-abcdefghijklmnopqrstuvwxyz123456 please"},
		},
		{
			ID:   "msg_real_0002",
			Kind: schema.KindText,
			Seq:  8,
			Role: schema.RoleAssistant,
			Thread: &schema.Thread{
				ThreadID:      "thread_sub_1",
				ParentBlockID: "msg_real_0001",
				AgentID:       "agent_secret_name",
				IsSidechain:   true,
			},
			Provenance: &schema.Provenance{DerivedFrom: []string{"msg_real_0001"}},
			Text:       &schema.TextBody{Text: "done"},
		},
	}
}

func TestProcessNormalizesAndRedacts(t *testing.T) {
	out, stats, verr := capturefixture.Process(rawCapture(), redaction.Default())
	if len(verr) != 0 {
		t.Fatalf("unexpected validation errors: %v", verr)
	}
	if stats.Blocks != 2 {
		t.Fatalf("blocks = %d, want 2", stats.Blocks)
	}

	// Identifiers are replaced with deterministic placeholders, and seq/ts are
	// re-derived from position.
	if out[0].ID != "blk-0001" || out[1].ID != "blk-0002" {
		t.Fatalf("block IDs not normalized: %q, %q", out[0].ID, out[1].ID)
	}
	if out[0].Seq != 0 || out[1].Seq != 1 {
		t.Fatalf("seq not re-derived: %d, %d", out[0].Seq, out[1].Seq)
	}
	if !out[1].TS.After(out[0].TS) {
		t.Fatalf("timestamps not monotonic from epoch: %v, %v", out[0].TS, out[1].TS)
	}

	// Referential integrity: both the thread parent and derived_from point at the
	// *normalized* ID of block 0.
	if got := out[1].Thread.ParentBlockID; got != "blk-0001" {
		t.Fatalf("thread parent not remapped: %q", got)
	}
	if got := out[1].Provenance.DerivedFrom; len(got) != 1 || got[0] != "blk-0001" {
		t.Fatalf("derived_from not remapped: %v", got)
	}
	// Real vendor/account identifiers are gone.
	if out[0].Provider.NativeID == "msg_native_99" || out[1].Thread.AgentID == "agent_secret_name" {
		t.Fatalf("real identifiers leaked: %+v / %+v", out[0].Provider, out[1].Thread)
	}
	// Body shape is preserved (still a text block), but the secret is scrubbed.
	if out[0].Text == nil || strings.Contains(out[0].Text.Text, "sk-ant-") {
		t.Fatalf("secret not redacted: %+v", out[0].Text)
	}
	if stats.RedactionSpans != 1 {
		t.Fatalf("redaction spans = %d, want 1", stats.RedactionSpans)
	}
}

func TestProcessIsDeterministic(t *testing.T) {
	a, _, _ := capturefixture.Process(rawCapture(), redaction.Default())
	b, _, _ := capturefixture.Process(rawCapture(), redaction.Default())
	if len(a) != len(b) {
		t.Fatalf("length mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID || !a[i].TS.Equal(b[i].TS) {
			t.Fatalf("non-deterministic at %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestProcessReportsInvalidBlocks(t *testing.T) {
	bad := []schema.Block{{Kind: schema.KindText}} // missing body for its kind
	_, _, verr := capturefixture.Process(bad, nil)
	if len(verr) == 0 {
		t.Fatal("expected a validation error for a kind/body mismatch")
	}
}

func TestMetadataValidate(t *testing.T) {
	ok := capturefixture.Metadata{
		Source:          "real-capture",
		RedactionStatus: "redacted",
		Intent:          "tool-call round trip",
		Providers:       []string{"anthropic/messages"},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}

	for name, m := range map[string]capturefixture.Metadata{
		"bad source":  {Source: "leak", RedactionStatus: "redacted", Intent: "x", Providers: []string{"a"}},
		"bad status":  {Source: "real-capture", RedactionStatus: "maybe", Intent: "x", Providers: []string{"a"}},
		"no intent":   {Source: "real-capture", RedactionStatus: "redacted", Providers: []string{"a"}},
		"no provider": {Source: "real-capture", RedactionStatus: "redacted", Intent: "x"},
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
