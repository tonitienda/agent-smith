package redaction_test

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/redaction"
	"github.com/tonitienda/agent-smith/schema"
)

// The redactor satisfies eventlog.Redactor and a scrubbed content block stays
// live in the projection — redaction minimizes data without dropping the block,
// and replay/insights see the structural marker instead of the raw secret.
func TestRedactedBlockRoundTripsThroughProjection(t *testing.T) {
	var _ eventlog.Redactor = redaction.Default()

	l := eventlog.New()
	l.SetRedactor(redaction.Default())
	if _, err := l.Append(schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindText,
		Role: schema.RoleUser,
		Text: &schema.TextBody{Text: "my key is sk-proj-abcdEFGH1234567890 keep this"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	p := projection.Project(l.Events(), projection.Options{})
	blocks := p.Blocks()
	if len(blocks) != 1 {
		t.Fatalf("want 1 projected block, got %d", len(blocks))
	}
	b := blocks[0]
	if !b.Live {
		t.Fatalf("redacted content block should stay live, reason=%q", b.Reason)
	}
	if strings.Contains(b.Text.Text, "sk-proj-abcdEFGH1234567890") {
		t.Fatalf("raw secret visible in projection: %q", b.Text.Text)
	}
	if _, ok := b.Ext[redaction.ExtKey]; !ok {
		t.Fatalf("projection lost the redaction marker")
	}
}
