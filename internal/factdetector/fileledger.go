package factdetector

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// FileLedger is a durable Ledger backed by a single JSON file, so a dismissed
// fact stays dismissed and the precision tally survives a process restart (the
// AS-108 durability follow-on to the in-memory MemLedger). It is the placeholder
// home until the cross-session rollup (AS-050) gains a backing store; the on-disk
// shape is deliberately small and additive (D2) — new fields can be appended
// without breaking an older reader.
//
// It stays free of memory/skill imports (the resolver injects those from the
// consumer): a ledger only needs the fingerprint, not where a fact would be saved.
type FileLedger struct {
	path      string
	mu        sync.Mutex
	dismissed map[string]bool
	stats     Stats
	err       error // last persist failure, exposed via Err
}

// ledgerFile is the persisted shape. Stats is embedded by value so its Accepted
// and Dismissed counts marshal as plain keys; appending a field here is backward
// compatible because json ignores unknown keys on read.
type ledgerFile struct {
	Dismissed []string `json:"dismissed"`
	Stats     Stats    `json:"stats"`
}

// OpenFileLedger loads the ledger at path, returning an empty one when the file
// does not yet exist (a first run). A malformed or unreadable existing file is an
// error so a corrupted ledger fails loudly rather than silently re-offering every
// previously dismissed fact.
func OpenFileLedger(path string) (*FileLedger, error) {
	l := &FileLedger{path: path, dismissed: map[string]bool{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read fact ledger %s: %w", path, err)
	}
	var lf ledgerFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse fact ledger %s: %w", path, err)
	}
	for _, fp := range lf.Dismissed {
		l.dismissed[fp] = true
	}
	l.stats = lf.Stats
	return l, nil
}

// Dismissed reports whether fingerprint was previously declined.
func (l *FileLedger) Dismissed(fingerprint string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dismissed[fingerprint]
}

// Record files an outcome and persists the ledger so the decision survives a
// restart. A persist error is surfaced through Err (the Ledger interface cannot
// return one); the in-memory state is still updated so the running process honors
// the dismissal regardless.
func (l *FileLedger) Record(fingerprint string, o Outcome) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch o {
	case Accepted:
		l.stats.Accepted++
	case Dismissed:
		l.stats.Dismissed++
		l.dismissed[fingerprint] = true
	}
	l.err = l.persist()
}

// Stats returns a snapshot of the precision tally.
func (l *FileLedger) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}

// Err returns the last persist failure, if any, so a caller that cares (a test
// or a face) can detect a ledger that failed to write its decision to disk.
func (l *FileLedger) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// persist writes the ledger atomically (temp file + rename) so a crash mid-write
// leaves the previous good file in place rather than a truncated one. Callers hold
// l.mu.
func (l *FileLedger) persist() error {
	dismissed := make([]string, 0, len(l.dismissed))
	for fp := range l.dismissed {
		dismissed = append(dismissed, fp)
	}
	data, err := json.Marshal(ledgerFile{Dismissed: dismissed, Stats: l.stats})
	if err != nil {
		return fmt.Errorf("marshal fact ledger: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create fact ledger dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".fact-ledger-*.tmp")
	if err != nil {
		return fmt.Errorf("create fact ledger temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write fact ledger temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close fact ledger temp: %w", err)
	}
	if err := os.Rename(tmpName, l.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename fact ledger: %w", err)
	}
	return nil
}
