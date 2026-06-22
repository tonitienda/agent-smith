package eventlog

import (
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// TestEscalationRoundTrips covers the additive escalation event (AS-116): the
// payload built by NewEscalation decodes back through EscalationOf, the kind is
// the dedicated control kind, and a non-escalation block is rejected.
func TestEscalationRoundTrips(t *testing.T) {
	want := Escalation{
		Feature: "compact",
		From:    "cheap",
		To:      "standard",
		Reason:  "the summarizer returned an empty summary",
	}
	b := NewEscalation("compact", want)
	if b.Kind != KindEscalation {
		t.Fatalf("kind = %q, want %q", b.Kind, KindEscalation)
	}
	if b.Provenance == nil || b.Provenance.Producer != "compact" {
		t.Fatalf("producer not attributed: %+v", b.Provenance)
	}
	got, ok := EscalationOf(b)
	if !ok {
		t.Fatal("EscalationOf(escalation event) = false, want true")
	}
	if got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}
	// A block of any other kind is not an escalation.
	if _, ok := EscalationOf(schema.Block{Kind: KindUsage}); ok {
		t.Error("EscalationOf(usage event) = true, want false")
	}
}
