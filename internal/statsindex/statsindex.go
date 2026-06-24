// Package statsindex is the disposable, derived cache behind `smith stats`
// (AS-136). AS-057 ships the portfolio analytics by re-opening and re-pricing
// every session on every call, which grows linearly with the corpus. This
// package persists the priced per-session rows (the input to stats.Build) so a
// later call re-prices only the sessions that changed since they were last seen.
//
// The index is never load-bearing: a missing, unreadable, version-mismatched, or
// pricing-stale file simply yields an empty index, and the caller falls back to
// pricing every session — exactly the AS-057 behaviour. That is what keeps the
// index safe to delete and reconstruct (`smith stats rebuild`): the append-only
// session logs remain the source of truth, and a stale index can only cost time,
// never correctness.
//
// Like the analytics it accelerates, everything here is local and offline: the
// index is a single JSON file under the state root that never leaves the machine.
package statsindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/stats"
)

// schemaVersion guards the cached row shape. Bumping it (or any change to the
// pricing fingerprint) invalidates the whole index on the next load, so a format
// change degrades to a recompute rather than serving rows from an older shape.
const schemaVersion = 1

// entry is one cached priced session: the row stats.Build consumes, tagged with
// the source session's change fingerprint so a modified session is detected and
// re-priced instead of served stale.
type entry struct {
	Fingerprint string        `json:"fp"`
	Session     stats.Session `json:"session"`
}

// file is the on-disk index document. Pricing is the table fingerprint the rows
// were priced under, so editing a rate invalidates the cached dollar figures.
type file struct {
	Version int              `json:"version"`
	Pricing string           `json:"pricing"`
	Entries map[string]entry `json:"entries"`
}

// Index is a loaded, in-memory view of the cache keyed by session directory (a
// stable, unique identity across projects). The zero value is unusable; use Load.
type Index struct {
	path    string
	pricing string
	entries map[string]entry
	dirty   bool
}

// Load reads the index at path for the given pricing fingerprint. A missing,
// unreadable, malformed, version-mismatched, or pricing-stale file yields an
// empty index — the index is disposable, so a miss is never an error, it just
// means everything is re-priced and the fresh rows are written back on Save.
func Load(path, pricing string) *Index {
	idx := &Index{path: path, pricing: pricing, entries: map[string]entry{}}
	b, err := os.ReadFile(path) //nolint:gosec // path is derived from the state root, not user input
	if err != nil {
		return idx
	}
	var f file
	if json.Unmarshal(b, &f) != nil {
		return idx
	}
	if f.Version != schemaVersion || f.Pricing != pricing || f.Entries == nil {
		return idx
	}
	idx.entries = f.Entries
	return idx
}

// Lookup returns the cached priced session for dir when present and its
// fingerprint still matches — i.e. the session has not changed since it was
// priced. A mismatch (or absence) misses, so the caller re-prices it.
func (i *Index) Lookup(dir, fp string) (stats.Session, bool) {
	e, ok := i.entries[dir]
	if !ok || e.Fingerprint != fp {
		return stats.Session{}, false
	}
	return e.Session, true
}

// Put records the priced session for dir under fingerprint fp, marking the index
// dirty so the next Save persists it.
func (i *Index) Put(dir, fp string, s stats.Session) {
	i.entries[dir] = entry{Fingerprint: fp, Session: s}
	i.dirty = true
}

// Reset drops every cached row, so a rebuild reconstructs the index from scratch
// and entries for since-deleted sessions do not linger.
func (i *Index) Reset() {
	i.entries = map[string]entry{}
	i.dirty = true
}

// Save writes the index to disk if it changed, atomically (temp file + rename) so
// a crash mid-write never leaves a half-written cache a future Load would reject.
// It is a no-op for an unchanged or path-less index.
func (i *Index) Save() error {
	if !i.dirty || i.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(i.path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(file{Version: schemaVersion, Pricing: i.pricing, Entries: i.entries})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(i.path), filepath.Base(i.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, i.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	i.dirty = false
	return nil
}

// Fingerprint is a session's cheap change signature, derived from summary stats
// the listing already computed (no log read): its last-active time, byte size,
// and event count. Any append to the log moves all three, so an unchanged session
// is served from the index while a modified one is re-priced.
func Fingerprint(updatedAt time.Time, sizeBytes int64, eventCount int) string {
	return fmt.Sprintf("%d-%d-%d", updatedAt.UnixNano(), sizeBytes, eventCount)
}

// PricingFingerprint is a stable signature of the pricing table, so editing a
// rate (or adding a model) invalidates the whole index — the cached dollar
// figures were computed under the old rates and would otherwise be served stale.
func PricingFingerprint(models []cost.Rate) string {
	ms := append([]cost.Rate(nil), models...)
	sort.Slice(ms, func(a, b int) bool { return ms[a].Model < ms[b].Model })
	b, _ := json.Marshal(ms)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
