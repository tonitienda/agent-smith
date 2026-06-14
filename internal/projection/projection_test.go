package projection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// text builds a minimal valid text block with a fixed ID (tests need stable,
// deterministic IDs; the log assigns Seq on append, which the projection does
// not depend on).
func text(id, body string) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: schema.RoleAssistant,
		Text: &schema.TextBody{Text: body},
	}
}

// mustAppend appends b to l, failing the test on error, and returns the stored
// block (with its assigned Seq).
func mustAppend(t *testing.T, l *eventlog.Log, b schema.Block) schema.Block {
	t.Helper()
	stored, err := l.Append(b)
	if err != nil {
		t.Fatalf("append %s: %v", b.ID, err)
	}
	return stored
}

// liveIDs returns the IDs of the live (model-facing) blocks in order.
func liveIDs(p *Projection) []string {
	var ids []string
	for _, b := range p.Live() {
		ids = append(ids, b.ID)
	}
	return ids
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNoEditEqualsRawConversation: a log with no exclusion/derived events
// projects to exactly its content blocks in order (AC #1).
func TestNoEditEqualsRawConversation(t *testing.T) {
	events := []schema.Block{text("a", "hi"), text("b", "there"), text("c", "world")}
	p := Project(events, Options{})

	if got, want := liveIDs(p), []string{"a", "b", "c"}; !eq(got, want) {
		t.Fatalf("live = %v, want %v", got, want)
	}
	if p.Len() != 3 || p.LiveLen() != 3 {
		t.Fatalf("Len/LiveLen = %d/%d, want 3/3", p.Len(), p.LiveLen())
	}
	for _, b := range p.Blocks() {
		if !b.Live || b.Reason != "" || len(b.ExcludedBy) != 0 {
			t.Fatalf("block %s: expected live with no exclusion, got live=%v reason=%q by=%v", b.ID, b.Live, b.Reason, b.ExcludedBy)
		}
	}
}

// blockJSON serializes b for byte-level prefix comparisons.
func blockJSON(t *testing.T, b schema.Block) string {
	t.Helper()
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal %s: %v", b.ID, err)
	}
	return string(out)
}

// TestLivePrefixStableAcrossAppend is the AS-011 prefix-stability invariant:
// appending a turn leaves every earlier live block byte-identical, so an
// unchanged prefix keeps hitting the provider cache.
func TestLivePrefixStableAcrossAppend(t *testing.T) {
	l := eventlog.New()
	for _, b := range []schema.Block{text("a", "1"), text("b", "2")} {
		mustAppend(t, l, b)
	}
	before := Project(l.Events(), Options{}).Live()

	mustAppend(t, l, text("c", "3"))
	after := Project(l.Events(), Options{}).Live()

	if len(after) <= len(before) {
		t.Fatalf("append did not grow the projection: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if blockJSON(t, before[i]) != blockJSON(t, after[i]) {
			t.Errorf("live block %d changed after append; prefix not stable", i)
		}
	}
}

// TestLivePrefixStableBeforeExclusion: a mid-session exclusion drops one block
// but leaves the blocks before it byte-identical, so the cache is invalidated
// only from the first changed block onward, never the whole prefix (AC #3).
func TestLivePrefixStableBeforeExclusion(t *testing.T) {
	l := eventlog.New()
	for _, b := range []schema.Block{text("a", "1"), text("b", "2"), text("c", "3")} {
		mustAppend(t, l, b)
	}
	before := Project(l.Events(), Options{}).Live()

	mustAppend(t, l, eventlog.NewExclusion("/clean", "b"))
	after := Project(l.Events(), Options{}).Live()

	if ids := liveIDs(Project(l.Events(), Options{})); !eq(ids, []string{"a", "c"}) {
		t.Fatalf("after exclusion live = %v, want [a c]", ids)
	}
	// "a" precedes the excluded "b": it must be untouched in both content and order.
	if blockJSON(t, before[0]) != blockJSON(t, after[0]) {
		t.Error("block before the excluded one changed; cache prefix should be intact")
	}
}

// TestExclusionRemovesFromProjectionOnly: excluding a block drops it from the
// projection but leaves the log untouched (AC #2).
func TestExclusionRemovesFromProjectionOnly(t *testing.T) {
	l := eventlog.New()
	for _, b := range []schema.Block{text("a", "1"), text("b", "2"), text("c", "3")} {
		if _, err := l.Append(b); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	excl, err := l.Append(eventlog.NewExclusion("/clean", "b"))
	if err != nil {
		t.Fatalf("append exclusion: %v", err)
	}

	p := Project(l.Events(), Options{})
	if got, want := liveIDs(p), []string{"a", "c"}; !eq(got, want) {
		t.Fatalf("live = %v, want %v", got, want)
	}

	// The excluded block is still rendered (for /context), marked excluded by
	// the exclusion event.
	var saw bool
	for _, b := range p.Blocks() {
		if b.ID == "b" {
			saw = true
			if b.Live || b.Reason != ReasonExcluded {
				t.Fatalf("b: live=%v reason=%q, want excluded", b.Live, b.Reason)
			}
			if !eq(b.ExcludedBy, []string{excl.ID}) {
				t.Fatalf("b.ExcludedBy = %v, want [%s]", b.ExcludedBy, excl.ID)
			}
		}
	}
	if !saw {
		t.Fatal("excluded block b absent from Blocks(); /context must still see it")
	}

	// The log itself is unchanged: every original event is still present.
	if l.Len() != 4 {
		t.Fatalf("log Len = %d, want 4 (3 content + 1 exclusion)", l.Len())
	}
	if b, ok := l.ByID("b"); !ok || len(b.ExcludedBy) != 0 {
		t.Fatalf("log event b mutated by projection: %+v (ok=%v)", b, ok)
	}
}

// TestUndoExclusionRestoresProjection: appending a counter-event that excludes
// the exclusion restores the projection exactly (AC #3).
func TestUndoExclusionRestoresProjection(t *testing.T) {
	l := eventlog.New()
	for _, b := range []schema.Block{text("a", "1"), text("b", "2"), text("c", "3")} {
		mustAppend(t, l, b)
	}
	before := liveIDs(Project(l.Events(), Options{}))

	excl := mustAppend(t, l, eventlog.NewExclusion("/clean", "b"))
	if got := liveIDs(Project(l.Events(), Options{})); !eq(got, []string{"a", "c"}) {
		t.Fatalf("after exclusion live = %v, want [a c]", got)
	}

	// Undo: an exclusion targeting the first exclusion nullifies it.
	mustAppend(t, l, eventlog.NewExclusion("/clean undo", excl.ID))
	after := Project(l.Events(), Options{})
	if got := liveIDs(after); !eq(got, before) {
		t.Fatalf("after undo live = %v, want %v", got, before)
	}
	// b is fully restored: live, no reason, no excluding IDs.
	for _, b := range after.Blocks() {
		if b.ID == "b" && (!b.Live || b.Reason != "" || len(b.ExcludedBy) != 0) {
			t.Fatalf("b not restored: live=%v reason=%q by=%v", b.Live, b.Reason, b.ExcludedBy)
		}
	}
}

// TestNestedUndo: undo composes to any depth — excluding the undo re-applies
// the original exclusion.
func TestNestedUndo(t *testing.T) {
	l := eventlog.New()
	mustAppend(t, l, text("a", "1"))
	mustAppend(t, l, text("b", "2"))

	e1 := mustAppend(t, l, eventlog.NewExclusion("clean", "b"))  // exclude b
	e2 := mustAppend(t, l, eventlog.NewExclusion("undo", e1.ID)) // undo: b back
	mustAppend(t, l, eventlog.NewExclusion("redo", e2.ID))       // undo the undo: b gone again

	if got := liveIDs(Project(l.Events(), Options{})); !eq(got, []string{"a"}) {
		t.Fatalf("after redo live = %v, want [a]", got)
	}
}

// TestDerivedBlockReplacesSources: a derived (compaction) block renders its own
// content and excludes the sources it was computed from.
func TestDerivedBlockReplacesSources(t *testing.T) {
	l := eventlog.New()
	mustAppend(t, l, text("a", "long turn 1"))
	mustAppend(t, l, text("b", "long turn 2"))
	mustAppend(t, l, text("c", "recent"))

	summary := schema.Block{
		ID:   "sum",
		Kind: schema.KindCompaction,
		Role: schema.RoleHarness,
		Text: &schema.TextBody{Text: "summary of a+b"},
	}
	mustAppend(t, l, eventlog.Derive(summary, "/compact", "a", "b"))

	p := Project(l.Events(), Options{})
	if got, want := liveIDs(p), []string{"c", "sum"}; !eq(got, want) {
		t.Fatalf("live = %v, want %v (sources excluded, summary present)", got, want)
	}

	// Undo the compaction by excluding the derived block: sources reappear, the
	// summary drops.
	mustAppend(t, l, eventlog.NewExclusion("/compact undo", "sum"))
	if got, want := liveIDs(Project(l.Events(), Options{})), []string{"a", "b", "c"}; !eq(got, want) {
		t.Fatalf("after undo live = %v, want %v", got, want)
	}
}

// TestPointInTime: projecting as of event n ignores any later counter-event
// (AC #4) — the basis for /rewind.
func TestPointInTime(t *testing.T) {
	events := []schema.Block{
		text("a", "1"),
		text("b", "2"),
		eventlog.NewExclusion("/clean", "a"), // index 2
		text("c", "3"),                       // index 3
	}

	cases := []struct {
		n    int
		want []string
	}{
		{0, nil},
		{1, []string{"a"}},
		{2, []string{"a", "b"}},
		{3, []string{"b"}},      // exclusion of a now in effect
		{4, []string{"b", "c"}}, // c appended after
	}
	for _, tc := range cases {
		if got := liveIDs(ProjectAt(events, tc.n, Options{})); !eq(got, tc.want) {
			t.Errorf("ProjectAt(n=%d) live = %v, want %v", tc.n, got, tc.want)
		}
	}

	// Out-of-range n is clamped, not a panic.
	if got := liveIDs(ProjectAt(events, 99, Options{})); !eq(got, []string{"b", "c"}) {
		t.Errorf("ProjectAt(n=99) live = %v, want [b c]", got)
	}
	if got := liveIDs(ProjectAt(events, -1, Options{})); got != nil {
		t.Errorf("ProjectAt(n=-1) live = %v, want empty", got)
	}
}

// TestReplayScope: same-model-only reasoning drops when the target model
// differs; portable reasoning and an unset TargetModel always keep it.
func TestReplayScope(t *testing.T) {
	sameModel := schema.Block{
		ID: "r1", Kind: schema.KindReasoning, Role: schema.RoleAssistant,
		Provider:  &schema.Provider{Model: "claude-x"},
		Reasoning: &schema.ReasoningBody{Text: "think", ReplayScope: schema.ReplaySameModelOnly},
	}
	portable := schema.Block{
		ID: "r2", Kind: schema.KindReasoning, Role: schema.RoleAssistant,
		Provider:  &schema.Provider{Model: "claude-x"},
		Reasoning: &schema.ReasoningBody{Text: "think", ReplayScope: schema.ReplayPortable},
	}
	events := []schema.Block{text("a", "1"), sameModel, portable}

	// No target model: everything projects (deterministic, model-agnostic).
	if got := liveIDs(Project(events, Options{})); !eq(got, []string{"a", "r1", "r2"}) {
		t.Fatalf("no-target live = %v, want all", got)
	}
	// Same model: same-model-only block is kept.
	if got := liveIDs(Project(events, Options{TargetModel: "claude-x"})); !eq(got, []string{"a", "r1", "r2"}) {
		t.Fatalf("same-model live = %v, want all", got)
	}
	// Different model: the same-model-only block drops, portable stays.
	p := Project(events, Options{TargetModel: "gpt-y"})
	if got := liveIDs(p); !eq(got, []string{"a", "r2"}) {
		t.Fatalf("cross-model live = %v, want [a r2]", got)
	}
	for _, b := range p.Blocks() {
		if b.ID == "r1" && (b.Live || b.Reason != ReasonReplayScope) {
			t.Fatalf("r1: live=%v reason=%q, want dropped by replay_scope", b.Live, b.Reason)
		}
	}
}

// TestExcludedTakesPrecedenceOverReplay: an excluded reasoning block reports
// the exclusion, not the replay scope (exclusion is checked first).
func TestExcludedTakesPrecedenceOverReplay(t *testing.T) {
	r := schema.Block{
		ID: "r1", Kind: schema.KindReasoning, Role: schema.RoleAssistant,
		Provider:  &schema.Provider{Model: "claude-x"},
		Reasoning: &schema.ReasoningBody{ReplayScope: schema.ReplaySameModelOnly},
	}
	events := []schema.Block{r, eventlog.NewExclusion("/clean", "r1")}
	for _, b := range Project(events, Options{TargetModel: "gpt-y"}).Blocks() {
		if b.ID == "r1" && b.Reason != ReasonExcluded {
			t.Fatalf("r1 reason = %q, want %q", b.Reason, ReasonExcluded)
		}
	}
}

// TestDeterministic: the same log yields byte-identical projections (AC #5).
func TestDeterministic(t *testing.T) {
	events := goldenLog()
	first, err := json.Marshal(Project(events, Options{}).Blocks())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, _ := json.Marshal(Project(events, Options{}).Blocks())
		if string(again) != string(first) {
			t.Fatalf("projection not deterministic on run %d", i)
		}
	}
}

// TestGolden pins the projection of a representative log. Regenerate with
// `go test ./internal/projection -run TestGolden -update`.
func TestGolden(t *testing.T) {
	path := filepath.Join("testdata", "projection.json")
	got, err := json.MarshalIndent(Project(goldenLog(), Options{}).Blocks(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("projection golden mismatch; rerun with -update if intended\n--- got ---\n%s", got)
	}
}

// goldenLog is a fixed log exercising live blocks, an exclusion + its undo, a
// derived compaction, and a still-active exclusion. IDs and timestamps are
// fixed so the golden file is byte-stable.
func goldenLog() []schema.Block {
	withTS := func(b schema.Block) schema.Block {
		b.TS = goldenTS
		return b
	}
	a := withTS(text("a", "first"))
	b := withTS(text("b", "second"))
	c := withTS(text("c", "third"))

	excl := withTS(eventlog.NewExclusion("/clean", "b"))
	excl.ID = "excl-b"
	undo := withTS(eventlog.NewExclusion("/clean undo", "excl-b"))
	undo.ID = "undo-excl-b"

	sum := withTS(eventlog.Derive(schema.Block{
		ID: "sum", Kind: schema.KindCompaction, Role: schema.RoleHarness,
		Text: &schema.TextBody{Text: "summary of a+c"},
	}, "/compact", "a", "c"))

	stillExcl := withTS(eventlog.NewExclusion("/clean", "c"))
	stillExcl.ID = "excl-c"

	return []schema.Block{a, b, c, excl, undo, sum, stillExcl}
}
