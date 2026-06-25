// Package skillrollup is the durable, cross-session surfacing layer for living
// skills (AS-050, PRD §7.20, Q9 resolution). The sub-agent analyzers — the
// rediscovered-fact detector (AS-048) and the skill-expectation-analyzer
// (AS-049) — report findings into the in-memory insights Store during a session;
// this package gives those findings a project-scoped home on disk so they
// compound across sessions: "skill X rediscovered the same fact in 4 sessions"
// is the signal a single session can never show.
//
// Store implements subagent.Store, so it drops in where the in-memory MemStore
// sat (the composition root swaps it in when a session store is present). Every
// recorded finding is also appended to a per-project JSONL log alongside the
// session store; Rollup reads the whole log back and aggregates it. Applying a
// remedy resolves a finding by appending a tombstone — the log is append-only and
// additive (D2): a Record carries only optional, json-tagged fields, and unknown
// fields are ignored on load, so a newer writer never breaks an older reader.
//
// Like the analyzers it surfaces, the rollup is deterministic and makes no model
// calls — it is the measured, grounded view (§7.20), free to open.
package skillrollup

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/subagent"
)

// EscalateSessions is the distinct-session threshold at which a recurring finding
// is visibly escalated (AS-050 AC: a fact rediscovered in 3+ sessions). The same
// fact surfacing across this many sessions is no longer noise — it is a durable
// gap the skill keeps failing to encode, so its urgency is promoted.
const EscalateSessions = 3

// Record is one persisted finding line. Every field beyond the kind/summary
// identity is optional and json-tagged, and json.Unmarshal silently drops unknown
// fields, so the log stays additive-only (D2): a newer writer can add fields a
// older reader simply ignores. A Record with Resolved set is a tombstone for its
// (Kind, Summary) signature — the remedy was applied, so the finding no longer
// pends.
type Record struct {
	Session    string    `json:"session,omitempty"`
	Span       string    `json:"span,omitempty"`
	SubAgent   string    `json:"subagent,omitempty"`
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary"`
	Detail     string    `json:"detail,omitempty"`
	Target     string    `json:"target,omitempty"`
	Diff       string    `json:"diff,omitempty"`
	Confidence int       `json:"confidence,omitempty"`
	Resolved   bool      `json:"resolved,omitempty"`
	RecordedAt time.Time `json:"recorded_at,omitempty"`
}

// Store is a durable, project-scoped findings store. It keeps every finding in
// memory for the live session (the subagent.Store contract /insights and the
// per-session view read) and mirrors each one to a JSONL log so the rollup
// survives across sessions. The zero value is invalid; use Open or NewMem.
type Store struct {
	mu   sync.Mutex
	path string // "" → in-memory only (no persistence)
	all  []Record
	seen map[string]bool // dedup key → present, so a re-run teardown is idempotent
}

// Open returns a Store backed by the JSONL log at path, loading any findings a
// prior session wrote so the rollup includes them. A missing file is an empty
// store, not an error. A corrupt line is skipped rather than aborting — a partial
// rollup beats no rollup.
func Open(path string) (*Store, error) {
	s := &Store{path: path, seen: map[string]bool{}}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // tolerate a corrupt line
		}
		s.add(r)
	}
	return s, sc.Err()
}

// NewMem returns an in-memory Store with no persistence — the fallback when no
// session store is present, and the seam tests use.
func NewMem() *Store { return &Store{seen: map[string]bool{}} }

// OpenMerged loads several per-project findings logs into one in-memory store so a
// single Rollup can span projects — the cross-project friction view (AS-136): a
// fact rediscovered across projects, not just sessions, is the broader signal a
// single project's log can never show. The merged store is read-only (no path);
// it is for reporting, so Record/Resolve would not persist. A missing log is an
// empty contribution, not an error (Open already tolerates it).
func OpenMerged(paths ...string) (*Store, error) {
	m := NewMem()
	for _, p := range paths {
		s, err := Open(p)
		if err != nil {
			return nil, err
		}
		for _, r := range s.all {
			m.add(r)
		}
	}
	return m, nil
}

// Record files a finding into memory and appends it to the durable log. A finding
// identical to one already recorded this process (same session/kind/summary/
// detail) is dropped, so a teardown that re-runs on an engine rebuild does not
// double-count.
func (s *Store) Record(f subagent.Finding) {
	r := Record{
		Session:    f.Session,
		Span:       f.Span,
		SubAgent:   f.SubAgent,
		Kind:       f.Kind,
		Summary:    f.Summary,
		Detail:     f.Detail,
		Confidence: f.Confidence,
		RecordedAt: time.Now().UTC(),
	}
	if f.Proposal != nil {
		r.Target = f.Proposal.Target
		r.Diff = f.Proposal.Description
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.add(r) {
		return
	}
	// A persistence failure leaves the in-memory record standing, so the live
	// session is unaffected; the rollup just won't see it after a restart. Swallow
	// it here (Record has no error return on the subagent.Store contract) — Resolve,
	// which the user drives, surfaces its write error instead.
	_ = s.append(r)
}

// Findings returns every finding recorded for a session, in record order — the
// subagent.Store contract, and the per-session view /skills renders. Tombstones
// are excluded; they are bookkeeping, not findings.
func (s *Store) Findings(session string) []subagent.Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []subagent.Finding
	for _, r := range s.all {
		if r.Resolved || r.Session != session {
			continue
		}
		f := subagent.Finding{
			SubAgent: r.SubAgent,
			Session:  r.Session,
			Span:     r.Span,
			Kind:     r.Kind,
			Summary:  r.Summary,
			Detail:   r.Detail,
		}
		if r.Diff != "" || r.Target != "" {
			f.Proposal = &subagent.Edit{Target: r.Target, Description: r.Diff}
		}
		out = append(out, f)
	}
	return out
}

// Resolve marks a (kind, summary) signature resolved by appending a tombstone, so
// a remedy applied once stays applied across sessions. It is idempotent.
func (s *Store) Resolve(kind, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Record{Kind: kind, Summary: summary, Resolved: true, RecordedAt: time.Now().UTC()}
	if !s.add(r) {
		return nil
	}
	return s.append(r)
}

// add records r in memory if it is new, returning whether it was added. It must
// be called under the lock (Open uses it single-threaded before publishing).
func (s *Store) add(r Record) bool {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	k := key(r)
	if s.seen[k] {
		return false
	}
	s.seen[k] = true
	s.all = append(s.all, r)
	return true
}

// append writes r to the durable log as one JSON line. A write failure is
// returned but the in-memory record already stands, so the live session is
// unaffected. A no-op when the store is in-memory only.
func (s *Store) append(r Record) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// key is a Record's dedup identity: a tombstone keys on its signature alone (one
// resolution per signature), a finding on its session and full content (the same
// finding re-reported in a later session is a distinct, countable occurrence).
func key(r Record) string {
	if r.Resolved {
		return "r\x00" + r.Kind + "\x00" + r.Summary
	}
	return "f\x00" + r.Session + "\x00" + r.Kind + "\x00" + r.Summary + "\x00" + r.Detail
}
