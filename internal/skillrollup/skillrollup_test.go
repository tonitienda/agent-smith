package skillrollup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/subagent"
)

// fact builds a rediscovered-fact-style finding for a session.
func fact(session, summary, target, diff string) subagent.Finding {
	return subagent.Finding{
		SubAgent: "rediscovered-fact-detector",
		Session:  session,
		Kind:     "rediscovered_fact",
		Summary:  summary,
		Detail:   "tried `x` before `y` worked",
		Proposal: &subagent.Edit{Target: target, Description: diff},
	}
}

// TestPersistAcrossStores asserts findings written by one store are read back by a
// fresh store opened on the same file — the cross-session compounding AS-050 needs.
func TestPersistAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-findings.jsonl")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s1.Record(fact("sess-1", "Rediscovered working command: make test", "AGENT.md", "+ `make test`"))

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := s2.Findings("sess-1"); len(got) != 1 || got[0].Summary != "Rediscovered working command: make test" {
		t.Fatalf("reopened store lost the finding: %+v", got)
	}
	if r := s2.Rollup(); r.Total != 1 || r.Sessions != 1 {
		t.Fatalf("rollup after reopen = %d findings / %d sessions, want 1/1", r.Total, r.Sessions)
	}
}

// TestRecordDedup asserts the same finding re-reported within a process (an engine
// rebuild re-running teardown) is counted once.
func TestRecordDedup(t *testing.T) {
	s := NewMem()
	f := fact("sess-1", "Rediscovered working command: make test", "AGENT.md", "+ `make test`")
	s.Record(f)
	s.Record(f)
	if got := s.Findings("sess-1"); len(got) != 1 {
		t.Fatalf("dedup failed: %d findings, want 1", len(got))
	}
}

// TestEscalationAtThreshold asserts a fact rediscovered in 3 distinct sessions is
// escalated, while one seen in 2 is not (AS-050 AC: 3+ sessions).
func TestEscalationAtThreshold(t *testing.T) {
	s := NewMem()
	summary := "Rediscovered working command: make test"
	for _, sess := range []string{"a", "b"} {
		s.Record(fact(sess, summary, "AGENT.md", "+ `make test`"))
	}
	if g := groupFor(t, s.Rollup(), summary); g.Escalated {
		t.Fatalf("2 sessions should not escalate")
	}
	s.Record(fact("c", summary, "AGENT.md", "+ `make test`"))
	g := groupFor(t, s.Rollup(), summary)
	if !g.Escalated || g.Sessions != 3 {
		t.Fatalf("3 sessions should escalate: %+v", g)
	}
}

// TestResolveDropsPending asserts resolving a finding removes it from the pending
// remedies and survives a reopen (the tombstone is durable).
func TestResolveDropsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.jsonl")
	s, _ := Open(path)
	summary := "Rediscovered working command: make test"
	s.Record(fact("a", summary, "AGENT.md", "+ `make test`"))
	if rep := s.Rollup(); len(rep.Pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(rep.Pending))
	}
	if err := s.Resolve("rediscovered_fact", summary); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rep := s.Rollup(); len(rep.Pending) != 0 {
		t.Fatalf("resolved finding still pends: %d", len(rep.Pending))
	}
	reopened, _ := Open(path)
	if rep := reopened.Rollup(); len(rep.Pending) != 0 {
		t.Fatalf("resolution did not survive reopen: %d pending", len(rep.Pending))
	}
	g := groupFor(t, reopened.Rollup(), summary)
	if !g.Resolved {
		t.Fatalf("group should be marked resolved after reopen: %+v", g)
	}
}

// TestUnknownFieldsTolerated asserts a log line carrying a field this version does
// not know is loaded rather than rejected — the additive-only (D2) guarantee.
func TestUnknownFieldsTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.jsonl")
	line := `{"session":"a","kind":"rediscovered_fact","summary":"Rediscovered working command: make test","future_field":{"nested":true},"score":0.9}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if rep := s.Rollup(); rep.Total != 1 {
		t.Fatalf("unknown-field line dropped: %d findings", rep.Total)
	}
}

// TestRenderShowsSessionAndRollup asserts Render surfaces both the per-session view
// and the cross-session escalation/remedy lines AS-050 requires.
func TestRenderShowsSessionAndRollup(t *testing.T) {
	s := NewMem()
	summary := "Rediscovered working command: make test"
	for _, sess := range []string{"a", "b", "c"} {
		s.Record(fact(sess, summary, "AGENT.md", "+ `make test` — working command"))
	}
	out := Render(s.Rollup(), s.Findings("c"), "c")
	for _, want := range []string{"This session (c)", summary, "escalated", "Pending remedies", "apply: /skills apply 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q\n%s", want, out)
		}
	}
}

func groupFor(t *testing.T, r Report, summary string) Group {
	t.Helper()
	for _, g := range r.Groups {
		if g.Summary == summary {
			return g
		}
	}
	t.Fatalf("no group for %q in %+v", summary, r.Groups)
	return Group{}
}
