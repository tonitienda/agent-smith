package eventlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// upper is a trivial Redactor that uppercases a text body, standing in for the
// real internal/redaction scrubber so this package stays leaf (schema-only).
type upper struct{}

func (upper) Redact(b schema.Block) (schema.Block, bool) {
	if b.Text == nil {
		return b, false
	}
	nt := *b.Text
	nt.Text = strings.ToUpper(nt.Text)
	b.Text = &nt
	return b, true
}

func TestSetRedactorScrubsBeforePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	l.SetRedactor(upper{})

	stored, err := l.Append(textBlock(schema.NewID(), "secret-token"))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if stored.Text.Text != "SECRET-TOKEN" {
		t.Fatalf("returned block not redacted: %q", stored.Text.Text)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The raw body must never have reached disk.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Fatalf("raw secret persisted to disk: %s", raw)
	}
	if !strings.Contains(string(raw), "SECRET-TOKEN") {
		t.Fatalf("redacted body missing from disk: %s", raw)
	}
}

func TestNilRedactorLeavesBlockUntouched(t *testing.T) {
	l := New()
	stored, err := l.Append(textBlock(schema.NewID(), "as-is"))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if stored.Text.Text != "as-is" {
		t.Fatalf("block changed with no redactor: %q", stored.Text.Text)
	}
}
