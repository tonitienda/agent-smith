package improve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DefaultSnooze is how long a snoozed proposal stays suppressed before it is
// offered again — long enough to clear the current focus, short enough that a
// still-valid gap resurfaces on its own.
const DefaultSnooze = 7 * 24 * time.Hour

// Status is a user's standing decision on a proposal.
type Status string

const (
	// Dismissed suppresses a proposal indefinitely — the user ruled it out. It is
	// superseded only when the proposed edit changes (the Key changes), so a refined
	// remedy is offered afresh.
	Dismissed Status = "dismissed"
	// Snoozed suppresses a proposal until Until — the user wants to revisit it later.
	Snoozed Status = "snoozed"
)

// Decision is one remembered ruling, keyed by a Proposal Key (target +
// normalized edit). The latest decision for a key wins, so a snooze can later be
// turned into a dismissal (or vice versa) and a superseding edit simply keys
// differently.
type Decision struct {
	Key    string    `json:"key"`
	Status Status    `json:"status"`
	Until  time.Time `json:"until,omitempty"` // snooze expiry; zero for Dismissed
	At     time.Time `json:"at,omitempty"`
}

// Ledger is the durable record of dismissed/snoozed proposals, backed by a
// single JSON file alongside the project's sessions so a decision stays in force
// across sessions (the AS-058 "memory of the dismissal" requirement). The
// on-disk shape is small and additive (D2): new fields can be appended without
// breaking an older reader. The zero value is invalid; use OpenLedger or
// NewMemLedger.
type Ledger struct {
	path      string
	mu        sync.Mutex
	decisions map[string]Decision
	err       error // last persist failure, exposed via Err
}

// ledgerFile is the persisted shape — the decisions flattened to a slice.
type ledgerFile struct {
	Decisions []Decision `json:"decisions"`
}

// OpenLedger loads the ledger at path, returning an empty one when the file does
// not yet exist (a first run). A malformed or unreadable existing file is an
// error so a corrupted ledger fails loudly rather than silently re-offering
// every previously dismissed proposal.
func OpenLedger(path string) (*Ledger, error) {
	l := &Ledger{path: path, decisions: map[string]Decision{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read improve ledger %s: %w", path, err)
	}
	var lf ledgerFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse improve ledger %s: %w", path, err)
	}
	for _, d := range lf.Decisions {
		l.decisions[d.Key] = d
	}
	return l, nil
}

// NewMemLedger returns an in-memory ledger with no persistence — the fallback
// when no session store is present, and the seam tests use.
func NewMemLedger() *Ledger { return &Ledger{decisions: map[string]Decision{}} }

// Suppressed reports whether the proposal for key should be hidden right now: a
// dismissal hides it indefinitely, a snooze until its expiry passes.
func (l *Ledger) Suppressed(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	d, ok := l.decisions[key]
	if !ok {
		return false
	}
	switch d.Status {
	case Dismissed:
		return true
	case Snoozed:
		return now.Before(d.Until)
	}
	return false
}

// Dismiss records an indefinite dismissal for key and persists it.
func (l *Ledger) Dismiss(key string, now time.Time) error {
	return l.record(Decision{Key: key, Status: Dismissed, At: now.UTC()})
}

// Snooze records a snooze for key until the given time and persists it.
func (l *Ledger) Snooze(key string, until, now time.Time) error {
	return l.record(Decision{Key: key, Status: Snoozed, Until: until.UTC(), At: now.UTC()})
}

// record stores the latest decision for its key and persists the ledger. The
// in-memory state is updated regardless so the running process honors the
// decision even if the write fails; the failure is returned for the caller to
// surface and also retained on Err.
func (l *Ledger) record(d Decision) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.decisions[d.Key] = d
	l.err = l.persist()
	return l.err
}

// Err returns the last persist failure, if any.
func (l *Ledger) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// persist writes the ledger atomically (temp file + rename) so a crash mid-write
// leaves the previous good file in place. A no-op when the ledger is in-memory
// only. Callers hold l.mu.
func (l *Ledger) persist() error {
	if l.path == "" {
		return nil
	}
	decisions := make([]Decision, 0, len(l.decisions))
	for _, d := range l.decisions {
		decisions = append(decisions, d)
	}
	// Sort by key so the persisted file is byte-stable across saves (map iteration
	// order is random) — deterministic writes keep diffs and tests reproducible.
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Key < decisions[j].Key })
	data, err := json.Marshal(ledgerFile{Decisions: decisions})
	if err != nil {
		return fmt.Errorf("marshal improve ledger: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create improve ledger dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".improve-ledger-*.tmp")
	if err != nil {
		return fmt.Errorf("create improve ledger temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write improve ledger temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync improve ledger temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close improve ledger temp: %w", err)
	}
	if err := os.Rename(tmpName, l.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename improve ledger: %w", err)
	}
	return nil
}
