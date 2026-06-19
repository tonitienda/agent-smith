package subagent

import "sync"

// Scope identifies one lifecycle window handed to a sub-agent's init and
// teardown. A span-scoped run carries the span id; a session-scoped run leaves
// Span empty. Session is always set so a finding can be attributed and a Store
// can group by session.
type Scope struct {
	// Kind is the window's granularity (span or session).
	Kind ScopeKind
	// Session is the owning session id, always set.
	Session string
	// Span is the span id for a span scope; empty for a session scope.
	Span string
}

// Edit is a propose-only change a sub-agent attaches to a finding when it holds
// the ProposeEdit permission. The framework records it for the user to confirm;
// it is never applied automatically (D9, C.5) — a sub-agent has no write path.
type Edit struct {
	// Target names what the edit would touch (a file path, a memory file, …).
	Target string
	// Description is the human-readable proposed change.
	Description string
}

// Finding is a single observation a sub-agent reports from teardown analysis. It
// is the unit the insights Store keeps and AS-045's /insights surfaces. Findings
// live in the Store, never on the event log, so an enabled analyzer adds no
// content blocks and a disabled one adds nothing at all.
type Finding struct {
	// SubAgent is the reporting sub-agent's name.
	SubAgent string
	// Session and Span attribute the finding to its scope (Span empty for session
	// scope).
	Session string
	Span    string
	// Kind is the finding kind, drawn from the manifest's Emits.
	Kind string
	// Summary is a one-line description; Detail is optional longer context.
	Summary string
	Detail  string
	// Proposal is an optional propose-only edit; non-nil only when the sub-agent
	// declared ProposeEdit.
	Proposal *Edit
}

// Store is where sub-agent findings report (the insights store of §7.19). It is
// an interface so AS-045 / AS-057 can back it with something durable later;
// AS-044 ships the in-memory MemStore, which keeps findings off the event log and
// out of the token budget.
type Store interface {
	// Record files a finding.
	Record(Finding)
	// Findings returns every finding recorded for a session, in record order.
	Findings(session string) []Finding
}

// MemStore is an in-memory Store: the default sink for findings within a live
// session. It is safe for concurrent use so teardown work can run off the
// interactive hot path (a goroutine) without racing a reader.
type MemStore struct {
	mu        sync.Mutex
	bySession map[string][]Finding
}

// NewMemStore returns an empty in-memory Store.
func NewMemStore() *MemStore {
	return &MemStore{bySession: map[string][]Finding{}}
}

// Record files f under its session.
func (s *MemStore) Record(f Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bySession == nil {
		s.bySession = map[string][]Finding{}
	}
	s.bySession[f.Session] = append(s.bySession[f.Session], f)
}

// Findings returns a copy of the findings recorded for session, in record order,
// so a caller cannot mutate the store's slice.
func (s *MemStore) Findings(session string) []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.bySession[session]
	if len(src) == 0 {
		return nil
	}
	out := make([]Finding, len(src))
	copy(out, src)
	return out
}
