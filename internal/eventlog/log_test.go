package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// textBlock is a minimal valid content block for tests.
func textBlock(id, text string) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: schema.RoleAssistant,
		Text: &schema.TextBody{Text: text},
	}
}

// marshalEvents renders a slice of blocks to JSON for order- and
// value-equality assertions that sidestep time.Time's internal representation.
func marshalEvents(t *testing.T, blocks []schema.Block) string {
	t.Helper()
	b, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	return string(b)
}

func TestAppendAssignsMonotonicSeqAndPreservesOrder(t *testing.T) {
	l := New()
	for i, txt := range []string{"a", "b", "c"} {
		stored, err := l.Append(textBlock(schema.NewID(), txt))
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if stored.Seq != i {
			t.Fatalf("event %d got Seq %d, want %d", i, stored.Seq, i)
		}
		if stored.TS.IsZero() {
			t.Fatalf("event %d: append did not stamp a timestamp", i)
		}
	}
	if l.Len() != 3 {
		t.Fatalf("Len = %d, want 3", l.Len())
	}
	got := []string{}
	for i := 0; i < l.Len(); i++ {
		b, ok := l.At(i)
		if !ok {
			t.Fatalf("At(%d) not ok", i)
		}
		got = append(got, b.Text.Text)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("iteration order = %v, want %v", got, want)
	}
}

func TestByIDLookup(t *testing.T) {
	l := New()
	mid := schema.NewID()
	l.Append(textBlock(schema.NewID(), "first"))
	l.Append(textBlock(mid, "target"))
	l.Append(textBlock(schema.NewID(), "third"))

	got, ok := l.ByID(mid)
	if !ok {
		t.Fatalf("ByID(%q) not found", mid)
	}
	if got.Text.Text != "target" {
		t.Fatalf("ByID returned %q, want %q", got.Text.Text, "target")
	}
	if _, ok := l.ByID("blk_missing"); ok {
		t.Fatalf("ByID found a block that was never appended")
	}
}

func TestAppendRejectsInvalidAndDuplicate(t *testing.T) {
	l := New()

	if _, err := l.Append(schema.Block{Kind: schema.KindText, Text: &schema.TextBody{}}); err == nil {
		t.Fatalf("expected error appending a block with no id")
	}
	// A content kind whose matching body is absent violates the schema invariant.
	if _, err := l.Append(schema.Block{ID: schema.NewID(), Kind: schema.KindText}); err == nil {
		t.Fatalf("expected error appending a text block with no text body")
	}

	id := schema.NewID()
	if _, err := l.Append(textBlock(id, "ok")); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if _, err := l.Append(textBlock(id, "dup")); err == nil {
		t.Fatalf("expected error re-appending the same id (ids are never reused)")
	}
	if l.Len() != 1 {
		t.Fatalf("rejected appends must not change the log: Len = %d, want 1", l.Len())
	}
}

func TestDiskRoundTripIsIdentical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	l, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	a := mustAppend(t, l, textBlock(schema.NewID(), "hello"))
	b := mustAppend(t, l, toolCallBlock(schema.NewID(), "Read"))
	// An exclusion and a derived block, the two non-content event kinds.
	excl := mustAppend(t, l, NewExclusion("/clean", a.ID))
	summary := Derive(textBlock(schema.NewID(), "summary of earlier turns"), "/compact", a.ID, b.ID)
	summary.Kind = schema.KindCompaction
	der := mustAppend(t, l, summary)
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	before := []schema.Block{a, b, excl, der}
	if got, want := marshalEvents(t, reopened.Events()), marshalEvents(t, before); got != want {
		t.Fatalf("reloaded events differ.\n got: %s\nwant: %s", got, want)
	}
	// Lookups and append-monotonicity survive the reload.
	if _, ok := reopened.ByID(excl.ID); !ok {
		t.Fatalf("exclusion event lost across reload")
	}
	next := mustAppend(t, reopened, textBlock(schema.NewID(), "after reload"))
	if next.Seq != 4 {
		t.Fatalf("post-reload Seq = %d, want 4 (monotonic across reload)", next.Seq)
	}
}

func TestExclusionAndDerivedCarryProvenance(t *testing.T) {
	src1, src2 := schema.NewID(), schema.NewID()

	excl := NewExclusion("/clean", src1, src2)
	if excl.Kind != KindExclusion {
		t.Fatalf("exclusion kind = %q, want %q", excl.Kind, KindExclusion)
	}
	if excl.Provenance == nil || excl.Provenance.Producer != "/clean" {
		t.Fatalf("exclusion lost its producer: %+v", excl.Provenance)
	}
	if !reflect.DeepEqual(excl.Provenance.DerivedFrom, []string{src1, src2}) {
		t.Fatalf("exclusion derived_from = %v, want %v", excl.Provenance.DerivedFrom, []string{src1, src2})
	}
	if err := excl.Validate(); err != nil {
		t.Fatalf("exclusion must be a valid block: %v", err)
	}

	der := Derive(textBlock(schema.NewID(), "rolled up"), "/compact", src1, src2)
	der.Kind = schema.KindCompaction
	if der.Provenance.Producer != "/compact" {
		t.Fatalf("derived block lost its producer")
	}
	if !reflect.DeepEqual(der.Provenance.DerivedFrom, []string{src1, src2}) {
		t.Fatalf("derived_from = %v, want %v", der.Provenance.DerivedFrom, []string{src1, src2})
	}

	// Both survive a JSON round-trip with provenance intact.
	for _, b := range []schema.Block{excl, der} {
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got schema.Block
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(got.Provenance, b.Provenance) {
			t.Fatalf("provenance changed across round-trip:\n got %+v\nwant %+v", got.Provenance, b.Provenance)
		}
	}
}

// TestTornTrailingLineIsDiscarded simulates a process killed mid-append: a
// partial final line with no terminating newline. Reload must recover every
// previously written event and drop only the torn fragment.
func TestTornTrailingLineIsDiscarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.jsonl")

	l, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	a := mustAppend(t, l, textBlock(schema.NewID(), "durable one"))
	b := mustAppend(t, l, textBlock(schema.NewID(), "durable two"))
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Append a torn fragment (a half-written JSON line, no newline) the way a
	// crash would leave it.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("reopen for torn write: %v", err)
	}
	if _, err := f.WriteString(`{"id":"blk_torn","kind":"text","seq":2,`); err != nil {
		t.Fatalf("write torn fragment: %v", err)
	}
	f.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after torn write: %v", err)
	}
	defer reopened.Close()

	if got, want := marshalEvents(t, reopened.Events()), marshalEvents(t, []schema.Block{a, b}); got != want {
		t.Fatalf("torn reload did not preserve prior events.\n got: %s\nwant: %s", got, want)
	}
	if _, ok := reopened.ByID("blk_torn"); ok {
		t.Fatalf("torn fragment was not discarded")
	}

	// The torn tail must have been truncated, so the next append produces a
	// clean, parseable log rather than concatenating onto the fragment.
	c := mustAppend(t, reopened, textBlock(schema.NewID(), "after recovery"))
	if c.Seq != 2 {
		t.Fatalf("post-recovery Seq = %d, want 2", c.Seq)
	}
	reopened.Close()

	verify, err := Open(path)
	if err != nil {
		t.Fatalf("final reopen reported corruption after recovery: %v", err)
	}
	defer verify.Close()
	if verify.Len() != 3 {
		t.Fatalf("final log Len = %d, want 3", verify.Len())
	}
}

func TestCorruptCompleteLineIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.jsonl")
	// A complete line (newline-terminated) that is not valid JSON is genuine
	// corruption, not a torn write, and must surface as an error.
	if err := os.WriteFile(path, []byte("{not json}\n"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatalf("expected Open to report a corrupt complete line")
	}
}

func TestInMemoryLogHasNoDiskBacking(t *testing.T) {
	l := New()
	if _, err := l.Append(textBlock(schema.NewID(), "x")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close on in-memory log should be a no-op, got %v", err)
	}
}

func mustAppend(t *testing.T, l *Log, b schema.Block) schema.Block {
	t.Helper()
	stored, err := l.Append(b)
	if err != nil {
		t.Fatalf("append %s: %v", b.ID, err)
	}
	return stored
}

func toolCallBlock(id, name string) schema.Block {
	return schema.Block{
		ID:       id,
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: "tu_" + id, Name: name},
	}
}
