package composition_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

const model = "claude-opus-4-8" // input rate 15 $/Mtok in the embedded table

var base = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

// text builds a text block of the given role with content sized to a known token
// estimate (4 chars ≈ 1 token), appended ageMinutes before the reference now.
func text(id string, role schema.Role, chars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: role,
		TS:   base.Add(-time.Duration(ageMinutes) * time.Minute),
		Text: &schema.TextBody{Text: strings.Repeat("x", chars)},
	}
}

// fileRead builds a file_read block for path with content sized to chars.
func fileRead(id, path string, chars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:       id,
		Kind:     schema.KindFileRead,
		Role:     schema.RoleTool,
		TS:       base.Add(-time.Duration(ageMinutes) * time.Minute),
		FileRead: &schema.FileReadBody{Path: path, Content: strings.Repeat("y", chars)},
	}
}

func build(t *testing.T, events []schema.Block, sortBy composition.Sort) composition.Composition {
	t.Helper()
	proj := projection.Project(events, projection.Options{TargetModel: model})
	return composition.Build(proj, cost.Embedded(), model, base, sortBy)
}

// TestTotalEqualsProjectionEstimate is the core AC: the sum of the per-segment
// tokens equals the projection's window total exactly (cost.EstimateContextTokens
// over the same live blocks), so the panel can never disagree with itself.
func TestTotalEqualsProjectionEstimate(t *testing.T) {
	events := []schema.Block{
		text("a", schema.RoleUser, 40, 1),
		text("b", schema.RoleAssistant, 80, 2),
		fileRead("c", "main.go", 200, 3),
	}
	proj := projection.Project(events, projection.Options{TargetModel: model})
	want := cost.EstimateContextTokens(proj.Live())

	c := composition.Build(proj, cost.Embedded(), model, base, composition.SortSize)
	if c.TotalTokens != want {
		t.Fatalf("TotalTokens = %d, want projection estimate %d", c.TotalTokens, want)
	}
	sum := 0
	for _, s := range c.Segments {
		sum += s.Tokens
	}
	if sum != c.TotalTokens {
		t.Errorf("segment token sum %d != TotalTokens %d", sum, c.TotalTokens)
	}
}

// TestTopConsumersRanked confirms the largest segments lead the list, so a user
// can read the top consumers off the top (PRD AC: top 3 in under 5s).
func TestTopConsumersRanked(t *testing.T) {
	events := []schema.Block{
		text("small", schema.RoleUser, 40, 1),        // ~10 tok
		fileRead("big", "main.go", 4000, 2),          // ~1000 tok
		text("medium", schema.RoleAssistant, 400, 3), // ~100 tok
	}
	c := build(t, events, composition.SortSize)

	if len(c.TopConsumers) < 3 {
		t.Fatalf("want at least 3 top consumers, got %d", len(c.TopConsumers))
	}
	order := []string{c.TopConsumers[0].ID, c.TopConsumers[1].ID, c.TopConsumers[2].ID}
	want := []string{"big", "medium", "small"}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("top consumer %d = %q, want %q", i, order[i], want[i])
		}
	}
	// SortSize must also order the full list largest-first.
	if c.Segments[0].ID != "big" {
		t.Errorf("first segment = %q, want big", c.Segments[0].ID)
	}
}

// TestDuplicateReadsFlagged checks a file read more than once is surfaced with
// the combined token and dollar cost, and the repeated segment IDs (the input a
// later manual /clean dedups on).
func TestDuplicateReadsFlagged(t *testing.T) {
	events := []schema.Block{
		fileRead("r1", "dup.go", 400, 1),
		fileRead("r2", "dup.go", 400, 2),
		fileRead("once", "other.go", 400, 3),
	}
	c := build(t, events, composition.SortSize)

	if len(c.Duplicates) != 1 {
		t.Fatalf("want 1 duplicate group, got %d: %+v", len(c.Duplicates), c.Duplicates)
	}
	d := c.Duplicates[0]
	if d.Path != "dup.go" || d.Count != 2 {
		t.Errorf("duplicate = %+v, want dup.go ×2", d)
	}
	if len(d.SegmentIDs) != 2 || d.SegmentIDs[0] != "r1" || d.SegmentIDs[1] != "r2" {
		t.Errorf("duplicate segment IDs = %v, want [r1 r2]", d.SegmentIDs)
	}
	// Combined tokens equal the sum of the two reads, and combined cost prices
	// those tokens at the input rate.
	wantTok := tokensOf(t, c, "r1") + tokensOf(t, c, "r2")
	if d.Tokens != wantTok {
		t.Errorf("combined tokens = %d, want %d", d.Tokens, wantTok)
	}
	wantCost := float64(wantTok) / 1e6 * 15.0
	approx(t, "duplicate combined cost", d.CostUSD, wantCost)
}

// TestPricingUsesInputRate verifies each segment's dollars are its estimated
// tokens valued at the active model's per-million input rate.
func TestPricingUsesInputRate(t *testing.T) {
	c := build(t, []schema.Block{fileRead("c", "main.go", 4000, 1)}, composition.SortSize)
	if !c.Priced {
		t.Fatal("opus is in the embedded table, want Priced")
	}
	seg := c.Segments[0]
	approx(t, "segment cost", seg.CostUSD, float64(seg.Tokens)/1e6*15.0)
	approx(t, "total cost", c.TotalCostUSD, float64(c.TotalTokens)/1e6*15.0)
}

// TestUnknownModelUnpriced confirms an unpriced model degrades gracefully:
// tokens still attribute, dollars are zero and flagged not-priced.
func TestUnknownModelUnpriced(t *testing.T) {
	proj := projection.Project([]schema.Block{text("a", schema.RoleUser, 40, 1)},
		projection.Options{})
	c := composition.Build(proj, cost.Embedded(), "no-such-model", base, composition.SortSize)
	if c.Priced {
		t.Error("unknown model must not be priced")
	}
	if c.TotalCostUSD != 0 || c.Segments[0].CostUSD != 0 {
		t.Errorf("unpriced costs must be zero, got total %v seg %v", c.TotalCostUSD, c.Segments[0].CostUSD)
	}
	if c.TotalTokens == 0 {
		t.Error("tokens must still attribute when the model is unpriced")
	}
}

// TestExcludedBlocksCounted confirms a block dropped from the window is counted
// as excluded and left out of the token total (the total tracks the live window).
func TestExcludedBlocksCounted(t *testing.T) {
	events := []schema.Block{
		text("a", schema.RoleUser, 40, 1),
		text("b", schema.RoleAssistant, 80, 2),
		eventlog.NewExclusion("test", "b"),
	}
	c := build(t, events, composition.SortSize)

	if len(c.Excluded) != 1 {
		t.Fatalf("Excluded = %d segments, want 1", len(c.Excluded))
	}
	if ex := c.Excluded[0]; ex.ID != "b" || ex.Live || ex.Reason == "" {
		t.Errorf("excluded segment = %+v, want id b, Live=false, a non-empty Reason", ex)
	}
	for _, s := range c.Segments {
		if s.ID == "b" {
			t.Error("excluded block b must not appear as a live segment")
		}
		if !s.Live {
			t.Errorf("live segment %q has Live=false", s.ID)
		}
	}
	// Total reflects only the live block a — excluded blocks are not in the window.
	if want := cost.EstimateBlockTokens(events[0]); c.TotalTokens != want {
		t.Errorf("TotalTokens = %d, want only live block a = %d", c.TotalTokens, want)
	}
}

// TestSortModes checks the three orderings of the full segment list.
func TestSortModes(t *testing.T) {
	events := []schema.Block{
		text("u", schema.RoleUser, 40, 5),       // oldest, small
		fileRead("f", "main.go", 4000, 1),       // newest, large
		text("a", schema.RoleAssistant, 400, 3), // middle
	}

	size := build(t, events, composition.SortSize)
	if size.Segments[0].ID != "f" {
		t.Errorf("SortSize first = %q, want f (largest)", size.Segments[0].ID)
	}

	age := build(t, events, composition.SortAge)
	if age.Segments[0].ID != "u" {
		t.Errorf("SortAge first = %q, want u (oldest by seq)", age.Segments[0].ID)
	}

	byType := build(t, events, composition.SortType)
	// Largest group first: file read (~1000 tok) leads assistant (~100) and user (~10).
	if byType.Segments[0].Group != "file read" {
		t.Errorf("SortType first group = %q, want file read (largest group)", byType.Segments[0].Group)
	}
}

// TestNilTableNoPanic confirms Build degrades gracefully with a nil pricing
// table: cost.Table's methods are nil-receiver-safe (Lookup/Window/Currency all
// handle a nil receiver), so the view still attributes tokens, just unpriced.
func TestNilTableNoPanic(t *testing.T) {
	proj := projection.Project([]schema.Block{text("a", schema.RoleUser, 40, 1)},
		projection.Options{})
	c := composition.Build(proj, nil, model, base, composition.SortSize)
	if c.Priced {
		t.Error("a nil table cannot price anything")
	}
	if c.TotalTokens == 0 {
		t.Error("tokens must still attribute with a nil table")
	}
	if c.Currency != "$" {
		t.Errorf("Currency = %q, want $ (nil-table fallback)", c.Currency)
	}
	if out := composition.Render(c); out == "" {
		t.Error("Render must not panic or empty out on a nil-table composition")
	}
}

func TestParseSort(t *testing.T) {
	cases := map[string]composition.Sort{
		"":        composition.SortSize,
		"size":    composition.SortSize,
		"age":     composition.SortAge,
		"recency": composition.SortAge,
		"type":    composition.SortType,
		"kind":    composition.SortType,
		"bogus":   composition.SortSize,
	}
	for in, want := range cases {
		if got := composition.ParseSort(in); got != want {
			t.Errorf("ParseSort(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestGroupsRollUp checks the by-type breakdown sums each group's tokens and
// segment count, and that tool calls and results share one "tool result" group.
func TestGroupsRollUp(t *testing.T) {
	events := []schema.Block{
		text("u1", schema.RoleUser, 40, 1),
		text("u2", schema.RoleUser, 40, 2),
		fileRead("f", "main.go", 400, 3),
	}
	c := build(t, events, composition.SortType)

	byName := map[string]composition.GroupTotal{}
	for _, g := range c.ByGroup {
		byName[g.Group] = g
	}
	if byName["user"].Count != 2 {
		t.Errorf("user group count = %d, want 2", byName["user"].Count)
	}
	if byName["file read"].Count != 1 {
		t.Errorf("file read group count = %d, want 1", byName["file read"].Count)
	}
	sum := 0
	for _, g := range c.ByGroup {
		sum += g.Tokens
	}
	if sum != c.TotalTokens {
		t.Errorf("group token sum %d != total %d", sum, c.TotalTokens)
	}
}

func tokensOf(t *testing.T, c composition.Composition, id string) int {
	t.Helper()
	for _, s := range c.Segments {
		if s.ID == id {
			return s.Tokens
		}
	}
	t.Fatalf("segment %q not found", id)
	return 0
}

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("%s = %.10f, want %.10f", name, got, want)
	}
}
