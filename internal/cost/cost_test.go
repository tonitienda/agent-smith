package cost_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

func ptr(n int) *int { return &n }

// usageBlock builds a logged usage event the way the loop records one.
func usageBlock(model string, tok *schema.Tokens) schema.Block {
	return eventlog.NewUsage("agent-loop", "anthropic", model, "end_turn", tok, nil)
}

const eps = 1e-9

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s = %.10f, want %.10f", name, got, want)
	}
}

// TestEmbeddedLookup checks exact and longest-prefix matching, and that an
// unknown model is reported as not found (so accounting degrades gracefully).
func TestEmbeddedLookup(t *testing.T) {
	tbl := cost.Embedded()

	if c := tbl.Currency(); c != "USD" {
		t.Errorf("Currency = %q, want USD", c)
	}

	r, ok := tbl.Lookup("claude-opus-4-8")
	if !ok {
		t.Fatal("claude-opus-4-8 should match the claude-opus-4-* family")
	}
	if r.InputPerMTok != 15.0 || r.OutputPerMTok != 75.0 {
		t.Errorf("opus rate = %+v, want input 15 output 75", r)
	}

	if _, ok := tbl.Lookup("some-unknown-model"); ok {
		t.Error("unknown model must not match")
	}
	if _, ok := tbl.Lookup(""); ok {
		t.Error("empty model must not match")
	}
}

// TestLongestPrefixWins ensures a more specific pattern beats a broader one
// regardless of table order.
func TestLongestPrefixWins(t *testing.T) {
	tbl := cost.Embedded()
	// gpt-4o-mini-2024 must pick the gpt-4o-mini* entry (0.15 in), not gpt-4o*.
	r, ok := tbl.Lookup("gpt-4o-mini-2024-07-18")
	if !ok {
		t.Fatal("gpt-4o-mini variant should match")
	}
	if r.InputPerMTok != 0.15 {
		t.Errorf("input rate = %v, want 0.15 (gpt-4o-mini*, not gpt-4o*)", r.InputPerMTok)
	}
}

// sidechainUsage builds a rolled-up child usage event (AS-046): a usage block
// tagged with the child's AgentID on a sidechain thread, the way delegate rolls
// one onto the parent log.
func sidechainUsage(agentID, model string, tok *schema.Tokens) schema.Block {
	b := usageBlock(model, tok)
	b.Thread = &schema.Thread{AgentID: agentID, IsSidechain: true}
	return b
}

// TestSummarizeItemizesDelegatedSpend is the AS-120 itemization check: rolled-up
// sidechain turns are grouped per child in Summary.Delegated (turns + tokens +
// dollars), while still counting toward the grand total — a breakdown, not an
// addition. The parent's own turns never appear in Delegated.
func TestSummarizeItemizesDelegatedSpend(t *testing.T) {
	tbl := cost.Embedded()
	events := []schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(100), Output: ptr(50)}), // parent's own
		sidechainUsage("agent-A", "claude-opus-4-8", &schema.Tokens{Input: ptr(200), Output: ptr(10)}),
		sidechainUsage("agent-A", "claude-opus-4-8", &schema.Tokens{Input: ptr(300), Output: ptr(20)}),
		sidechainUsage("agent-B", "claude-opus-4-8", &schema.Tokens{Input: ptr(400), Output: ptr(30)}),
	}

	s := cost.Summarize(events, tbl)
	if len(s.Delegated) != 2 {
		t.Fatalf("delegated children = %d, want 2", len(s.Delegated))
	}

	a := s.Delegated[0]
	if a.AgentID != "agent-A" || a.Turns != 2 || a.Tokens.Input != 500 || a.Tokens.Output != 30 {
		t.Errorf("agent-A child cost = %+v, want 2 turns / input 500 / output 30", a)
	}
	b := s.Delegated[1]
	if b.AgentID != "agent-B" || b.Turns != 1 || b.Tokens.Input != 400 {
		t.Errorf("agent-B child cost = %+v, want 1 turn / input 400", b)
	}

	// The grand total still includes every turn (parent + both children): the
	// breakdown does not double-count.
	if s.Total.Input != 1000 || s.Total.Output != 110 {
		t.Errorf("grand total tokens = %+v, want input 1000 / output 110", s.Total)
	}
	delegatedUSD := a.TotalUSD + b.TotalUSD
	if delegatedUSD <= 0 || delegatedUSD >= s.TotalUSD {
		t.Errorf("delegated spend %v should be a positive share below the grand total %v", delegatedUSD, s.TotalUSD)
	}
}

// TestSummarizeReconcilesAndPrices verifies token totals reconcile exactly with
// the logged usage and the per-turn dollar math matches the rate table.
func TestSummarizeReconcilesAndPrices(t *testing.T) {
	tbl := cost.Embedded()
	events := []schema.Block{
		{Kind: schema.KindText, Text: &schema.TextBody{Text: "hi"}}, // ignored
		usageBlock("claude-opus-4-8", &schema.Tokens{
			Input: ptr(100), Output: ptr(60), CacheRead: ptr(40), CacheWrite: ptr(20),
		}),
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(900), Output: ptr(120)}),
	}

	s := cost.Summarize(events, tbl)
	if len(s.Turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(s.Turns))
	}
	if !s.AllPriced {
		t.Error("all turns should be priced")
	}

	// Token totals reconcile with the sum of the logged usage.
	if s.Total.Input != 1000 || s.Total.Output != 180 || s.Total.CacheRead != 40 || s.Total.CacheWrite != 20 {
		t.Errorf("token totals = %+v", s.Total)
	}

	// Turn 1 dollar breakdown against the opus rate (15/75/1.5/18.75 per MTok).
	t1 := s.Turns[0]
	approx(t, "input", t1.InputUSD, 100*15.0/1e6)
	approx(t, "output", t1.OutputUSD, 60*75.0/1e6)
	approx(t, "cacheRead", t1.CacheReadUSD, 40*1.5/1e6)
	approx(t, "cacheWrite", t1.CacheWriteUSD, 20*18.75/1e6)
	approx(t, "total", t1.TotalUSD, t1.InputUSD+t1.OutputUSD+t1.CacheReadUSD+t1.CacheWriteUSD)

	// Session total is the sum of the per-turn totals — exact reconciliation.
	approx(t, "session total", s.TotalUSD, s.Turns[0].TotalUSD+s.Turns[1].TotalUSD)
}

// TestCacheSavings checks the savings computation in tokens and dollars.
func TestCacheSavings(t *testing.T) {
	tbl := cost.Embedded()
	s := cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(10), CacheRead: ptr(40)}),
	}, tbl)

	if s.CacheReadTokens != 40 {
		t.Errorf("cache read tokens = %d, want 40", s.CacheReadTokens)
	}
	// Saved = cached tokens * (input rate - cache-read rate) = 40 * (15 - 1.5)/1e6.
	approx(t, "cache savings", s.CacheSavingsUSD, 40*(15.0-1.5)/1e6)
}

// TestUnknownModelDegrades checks that an unpriced model still reports exact
// tokens, marks the turn unpriced, and flips AllPriced false on the summary.
func TestUnknownModelDegrades(t *testing.T) {
	tbl := cost.Embedded()
	s := cost.Summarize([]schema.Block{
		usageBlock("mystery-model-9", &schema.Tokens{Input: ptr(500), Output: ptr(250)}),
	}, tbl)

	if s.AllPriced {
		t.Error("summary with an unknown model must not be AllPriced")
	}
	tc := s.Turns[0]
	if tc.Priced {
		t.Error("unknown-model turn must be unpriced")
	}
	if tc.Tokens.Input != 500 || tc.Tokens.Output != 250 {
		t.Errorf("tokens = %+v, want exact 500/250", tc.Tokens)
	}
	if tc.TotalUSD != 0 {
		t.Errorf("unpriced turn cost = %v, want 0", tc.TotalUSD)
	}
}

// TestNilTablePricesNothing guards the degenerate case: no table yields exact
// tokens but no dollars.
func TestNilTablePricesNothing(t *testing.T) {
	s := cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(10)}),
	}, nil)
	if s.AllPriced {
		t.Error("nil table cannot price any turn")
	}
	if s.Total.Input != 10 {
		t.Errorf("tokens lost with nil table: %+v", s.Total)
	}
	if s.Currency != "USD" {
		t.Errorf("nil table currency = %q, want USD fallback", s.Currency)
	}
}

// TestOverrideLayering checks that a $SMITH_PRICING file overrides the embedded
// rate for its models while unlisted models still fall back to the embedded
// table.
func TestOverrideLayering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.json")
	override := `{"version":1,"currency":"USD","models":[
		{"model":"claude-opus-4-*","input_per_mtok":1.0,"output_per_mtok":2.0}
	]}`
	if err := os.WriteFile(path, []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cost.EnvPricingFile, path)

	tbl, err := cost.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	// Overridden model takes the new rate.
	r, ok := tbl.Lookup("claude-opus-4-8")
	if !ok || r.InputPerMTok != 1.0 || r.OutputPerMTok != 2.0 {
		t.Errorf("override rate = %+v (ok=%v), want input 1 output 2", r, ok)
	}
	// A model not in the override still resolves from the embedded parent.
	if _, ok := tbl.Lookup("gpt-4o"); !ok {
		t.Error("non-overridden model should fall back to embedded table")
	}
}

// TestEmptyModelNeverPriced guards that an empty (unspecified) model is not
// priced even when an override contains a catch-all "*" pattern, which would
// otherwise match because strings.HasPrefix("", "") is true.
func TestEmptyModelNeverPriced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.json")
	catchAll := `{"version":1,"currency":"USD","models":[
		{"model":"*","input_per_mtok":1.0,"output_per_mtok":1.0}
	]}`
	if err := os.WriteFile(path, []byte(catchAll), 0o600); err != nil {
		t.Fatal(err)
	}
	tbl, err := cost.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	// A real model matches the catch-all...
	if _, ok := tbl.Lookup("anything"); !ok {
		t.Error("catch-all should price a named model")
	}
	// ...but an empty model is unspecified and must never be priced.
	if _, ok := tbl.Lookup(""); ok {
		t.Error("empty model must not match the catch-all pattern")
	}
}

// TestUnsupportedVersionRejected ensures an override carrying an unknown schema
// version is a reported error rather than a silent misparse.
func TestUnsupportedVersionRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.json")
	future := `{"version":2,"currency":"USD","models":[]}`
	if err := os.WriteFile(path, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cost.LoadFile(path); err == nil {
		t.Error("a version-2 pricing file should be rejected")
	}
	// A file with no version field (defaults to 0) is likewise rejected.
	missing := filepath.Join(dir, "noversion.json")
	if err := os.WriteFile(missing, []byte(`{"currency":"USD","models":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cost.LoadFile(missing); err == nil {
		t.Error("a pricing file without a version should be rejected")
	}
}

// TestDefaultMissingOverrideErrors ensures a typo'd override path is a reported
// error, never a silent fall-through that misprices a session.
func TestDefaultMissingOverrideErrors(t *testing.T) {
	t.Setenv(cost.EnvPricingFile, filepath.Join(t.TempDir(), "does-not-exist.json"))
	if _, err := cost.Default(); err == nil {
		t.Error("Default with an unreadable override file should error")
	}
}

// TestDefaultNoOverride returns the embedded table when the env var is unset.
func TestDefaultNoOverride(t *testing.T) {
	t.Setenv(cost.EnvPricingFile, "")
	tbl, err := cost.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if _, ok := tbl.Lookup("claude-opus-4-8"); !ok {
		t.Error("embedded table should price opus")
	}
}

// TestWindowLookup resolves the model context window the same way pricing does:
// longest-prefix match, with an unknown model reported as not found so the meter
// can fall back to a bare token count.
func TestWindowLookup(t *testing.T) {
	tbl := cost.Embedded()
	if w, ok := tbl.Window("claude-opus-4-8"); !ok || w != 200000 {
		t.Errorf("opus window = (%d, %v), want (200000, true)", w, ok)
	}
	if w, ok := tbl.Window("gpt-4.1-2025-04-14"); !ok || w != 1047576 {
		t.Errorf("gpt-4.1 window = (%d, %v), want (1047576, true)", w, ok)
	}
	if w, ok := tbl.Window("mystery-model-9"); ok || w != 0 {
		t.Errorf("unknown window = (%d, %v), want (0, false)", w, ok)
	}
	if w, ok := tbl.Window(""); ok || w != 0 {
		t.Errorf("empty model window = (%d, %v), want (0, false)", w, ok)
	}
}

// TestWindowUnsetIsUnknown confirms a priced model with no recorded window
// reports the window as unknown rather than zero-as-a-real-size.
func TestWindowUnsetIsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricing.json")
	doc := `{"version":1,"currency":"USD","models":[
		{"model":"local-llm","input_per_mtok":0,"output_per_mtok":0}]}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	tbl, err := cost.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := tbl.Lookup("local-llm"); !ok {
		t.Fatal("local-llm should be priced")
	}
	if w, ok := tbl.Window("local-llm"); ok || w != 0 {
		t.Errorf("window for entry with no context_window = (%d, %v), want (0, false)", w, ok)
	}
}

// TestLatestAndContextTokens checks that the most recent turn drives the live
// window occupancy: prompt (input + cache read + cache write) plus output.
func TestLatestAndContextTokens(t *testing.T) {
	if _, ok := cost.Summarize(nil, cost.Embedded()).Latest(); ok {
		t.Error("Latest on an empty summary should report no turn")
	}
	s := cost.Summarize([]schema.Block{
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(100), Output: ptr(10)}),
		usageBlock("claude-opus-4-8", &schema.Tokens{
			Input: ptr(900), CacheRead: ptr(8000), CacheWrite: ptr(200), Output: ptr(150),
		}),
	}, cost.Embedded())
	last, ok := s.Latest()
	if !ok {
		t.Fatal("Latest should report the most recent turn")
	}
	if last.Index != 2 {
		t.Errorf("Latest index = %d, want 2 (the last turn)", last.Index)
	}
	if got := last.ContextTokens(); got != 9250 {
		t.Errorf("ContextTokens = %d, want 9250 (900+8000+200+150)", got)
	}
}

// TestDefaultWithLayersConfigSection checks the AS-071 pricing layering: a
// config `pricing` section overrides the embedded defaults model-by-model, and a
// $SMITH_PRICING file overrides the config section in turn, with each layer
// falling back to the one below for models it does not name.
func TestDefaultWithLayersConfigSection(t *testing.T) {
	section := []byte(`{"version":1,"currency":"USD","models":[
		{"model":"claude-opus-4-8","input_per_mtok":3.0,"output_per_mtok":4.0}
	]}`)

	// Config section alone overrides the embedded rate; other models fall back.
	t.Setenv(cost.EnvPricingFile, "")
	tbl, err := cost.DefaultWith(section)
	if err != nil {
		t.Fatalf("DefaultWith: %v", err)
	}
	if r, ok := tbl.Lookup("claude-opus-4-8"); !ok || r.InputPerMTok != 3.0 {
		t.Errorf("config-section rate = %+v (ok=%v), want input 3", r, ok)
	}
	if _, ok := tbl.Lookup("gpt-4o"); !ok {
		t.Error("a model absent from the config section should fall back to embedded")
	}

	// A $SMITH_PRICING file outranks the config section for the models it names.
	path := filepath.Join(t.TempDir(), "pricing.json")
	envFile := `{"version":1,"currency":"USD","models":[
		{"model":"claude-opus-4-8","input_per_mtok":9.0,"output_per_mtok":9.0}
	]}`
	if err := os.WriteFile(path, []byte(envFile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cost.EnvPricingFile, path)
	tbl, err = cost.DefaultWith(section)
	if err != nil {
		t.Fatalf("DefaultWith with env file: %v", err)
	}
	if r, ok := tbl.Lookup("claude-opus-4-8"); !ok || r.InputPerMTok != 9.0 {
		t.Errorf("env-file rate = %+v (ok=%v), want input 9 (env outranks config)", r, ok)
	}
}

// TestDefaultWithInvalidSection ensures a malformed config pricing section is a
// reported error, never a silent fall-through that misprices a session.
func TestDefaultWithInvalidSection(t *testing.T) {
	t.Setenv(cost.EnvPricingFile, "")
	if _, err := cost.DefaultWith([]byte(`{"version":2,"models":[]}`)); err == nil {
		t.Error("a version-2 pricing section should be rejected")
	}
}
