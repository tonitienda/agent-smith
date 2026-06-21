package factdetector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/subagent"
)

// AC (AS-108): a dismissed fact is not re-offered in a fresh session — the ledger
// survives a process restart. Scripted as two sessions sharing one on-disk ledger
// file: the second opens a brand-new FileLedger and Detector, proving the decision
// came off disk rather than from in-process state.
func TestFileLedgerSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fact-ledger.json")

	// Session 1: the fact is offered, then the user dismisses it.
	led1, err := OpenFileLedger(path)
	if err != nil {
		t.Fatalf("open ledger (session 1): %v", err)
	}
	d1 := New(nil, led1)
	first := d1.Teardown(subagent.Scope{}, flailToFindTest()).Findings
	if len(first) != 1 {
		t.Fatalf("want 1 finding first session, got %d", len(first))
	}
	led1.Record(commandFingerprint("go test ./..."), Dismissed)
	if err := led1.Err(); err != nil {
		t.Fatalf("persist after dismiss: %v", err)
	}

	// Session 2: a fresh ledger and detector read the same file; the dismissed
	// fact must not be re-offered, and the precision tally must carry over.
	led2, err := OpenFileLedger(path)
	if err != nil {
		t.Fatalf("open ledger (session 2): %v", err)
	}
	d2 := New(nil, led2)
	second := d2.Teardown(subagent.Scope{}, flailToFindTest()).Findings
	if len(second) != 0 {
		t.Fatalf("dismissed fact re-offered after restart: %+v", second)
	}
	if s := led2.Stats(); s.Dismissed != 1 {
		t.Fatalf("precision tally not persisted: %+v", s)
	}
}

// A non-dismissed fact is still offered after a restart — persistence suppresses
// only what the user actually declined.
func TestFileLedgerKeepsOfferingUndismissed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fact-ledger.json")
	led1, err := OpenFileLedger(path)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	led1.Record(commandFingerprint("some other command"), Accepted)

	led2, err := OpenFileLedger(path)
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	if got := New(nil, led2).Teardown(subagent.Scope{}, flailToFindTest()).Findings; len(got) != 1 {
		t.Fatalf("undismissed fact should still be offered, got %d", len(got))
	}
	if s := led2.Stats(); s.Accepted != 1 {
		t.Fatalf("accepted tally not persisted: %+v", s)
	}
}

// A first run with no ledger file yet opens cleanly as an empty ledger rather
// than erroring.
func TestFileLedgerMissingFileIsEmpty(t *testing.T) {
	led, err := OpenFileLedger(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should be a clean empty ledger: %v", err)
	}
	if led.Dismissed("anything") {
		t.Fatal("empty ledger reports a dismissal")
	}
}

// A corrupted ledger file fails loudly so a session does not silently re-offer
// every previously dismissed fact.
func TestFileLedgerCorruptFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fact-ledger.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileLedger(path); err == nil {
		t.Fatal("corrupt ledger should error, not load empty")
	}
}
