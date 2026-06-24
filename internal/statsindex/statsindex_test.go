package statsindex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/stats"
)

func row(id string, usd float64) stats.Session {
	return stats.Session{ID: id, Project: "p", UpdatedAt: time.Unix(1, 0), Cost: cost.Summary{TotalUSD: usd, Currency: "USD", AllPriced: true}}
}

// TestRoundTrip asserts a saved index is read back: a matching fingerprint hits
// the cache, so the priced row survives a reload without re-pricing.
func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats-index.json")
	idx := Load(path, "pricing-v1")
	if _, ok := idx.Lookup("/a", "fp1"); ok {
		t.Fatal("empty index should miss")
	}
	idx.Put("/a", "fp1", row("a", 1.5))
	if err := idx.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := Load(path, "pricing-v1")
	ps, ok := got.Lookup("/a", "fp1")
	if !ok {
		t.Fatal("reloaded index should hit")
	}
	if ps.Cost.TotalUSD != 1.5 {
		t.Fatalf("row not preserved: %+v", ps)
	}
}

// TestFingerprintMismatchMisses asserts a changed session (different fingerprint)
// misses, so a modified session is re-priced instead of served stale.
func TestFingerprintMismatchMisses(t *testing.T) {
	idx := Load(filepath.Join(t.TempDir(), "i.json"), "p")
	idx.Put("/a", "fp1", row("a", 1))
	if _, ok := idx.Lookup("/a", "fp2"); ok {
		t.Fatal("stale fingerprint should miss")
	}
}

// TestPricingChangeInvalidates asserts editing the pricing table (a different
// pricing fingerprint) discards the whole index on load: cached dollar figures
// priced under the old rates must not be served.
func TestPricingChangeInvalidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "i.json")
	idx := Load(path, "pricing-v1")
	idx.Put("/a", "fp1", row("a", 1))
	if err := idx.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := Load(path, "pricing-v2")
	if _, ok := got.Lookup("/a", "fp1"); ok {
		t.Fatal("pricing change should invalidate the index")
	}
}

// TestResetDropsRows asserts Reset empties the cache so a rebuild does not carry
// rows for since-deleted sessions.
func TestResetDropsRows(t *testing.T) {
	idx := Load(filepath.Join(t.TempDir(), "i.json"), "p")
	idx.Put("/a", "fp1", row("a", 1))
	idx.Reset()
	if _, ok := idx.Lookup("/a", "fp1"); ok {
		t.Fatal("Reset should drop rows")
	}
}

// TestPricingFingerprintStability asserts the pricing fingerprint is order-
// independent (so model-map iteration order never spuriously invalidates the
// index) but changes when a rate changes.
func TestPricingFingerprintStability(t *testing.T) {
	a := []cost.Rate{{Model: "x", InputPerMTok: 1}, {Model: "y", OutputPerMTok: 2}}
	b := []cost.Rate{{Model: "y", OutputPerMTok: 2}, {Model: "x", InputPerMTok: 1}}
	if PricingFingerprint(a) != PricingFingerprint(b) {
		t.Fatal("fingerprint must be order-independent")
	}
	c := []cost.Rate{{Model: "x", InputPerMTok: 9}, {Model: "y", OutputPerMTok: 2}}
	if PricingFingerprint(a) == PricingFingerprint(c) {
		t.Fatal("a rate change must change the fingerprint")
	}
}

// TestMissingFileIsEmpty asserts a missing index file is an empty index, not an
// error — the index is disposable, so a fresh machine degrades to a full
// recompute.
func TestMissingFileIsEmpty(t *testing.T) {
	idx := Load(filepath.Join(t.TempDir(), "does-not-exist.json"), "p")
	if _, ok := idx.Lookup("/a", "fp"); ok {
		t.Fatal("missing file should yield an empty index")
	}
}
