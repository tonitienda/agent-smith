package improve

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/skillrollup"
)

// report builds a rollup report with the given groups for the table tests.
func report(groups ...skillrollup.Group) skillrollup.Report {
	return skillrollup.Report{Groups: groups}
}

func grp(summary string, sessions int) skillrollup.Group {
	return skillrollup.Group{
		Kind:     "fact",
		Summary:  summary,
		Sessions: sessions,
		Target:   "AGENT.md",
		Diff:     "- " + summary,
	}
}

func TestBuildPromotesOnlyRecurringFindings(t *testing.T) {
	rep := report(
		grp("recurs twice", 2),
		grp("single session", 1),
	)
	props := Build(rep, NewMemLedger(), time.Now())
	if len(props) != 1 {
		t.Fatalf("want 1 proposal (only the ≥%d-session one), got %d", MinSessions, len(props))
	}
	if props[0].Summary != "recurs twice" {
		t.Fatalf("wrong proposal promoted: %q", props[0].Summary)
	}
	if props[0].Index != 1 {
		t.Fatalf("want 1-based index 1, got %d", props[0].Index)
	}
}

func TestBuildSkipsResolvedTargetlessAndEditless(t *testing.T) {
	resolved := grp("already applied", 3)
	resolved.Resolved = true
	noTarget := grp("no target", 3)
	noTarget.Target = ""
	noEdit := grp("no edit", 3)
	noEdit.Diff = ""

	props := Build(report(resolved, noTarget, noEdit), NewMemLedger(), time.Now())
	if len(props) != 0 {
		t.Fatalf("want 0 proposals (all unactionable), got %d", len(props))
	}
}

func TestBuildSuppressesDismissed(t *testing.T) {
	rep := report(grp("recurs twice", 2))
	led := NewMemLedger()
	now := time.Now()

	props := Build(rep, led, now)
	if len(props) != 1 {
		t.Fatalf("precondition: want 1 proposal, got %d", len(props))
	}
	if err := led.Dismiss(Key(props[0].Target, props[0].Edit), now); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if got := Build(rep, led, now); len(got) != 0 {
		t.Fatalf("dismissed proposal should not reappear, got %d", len(got))
	}
}

func TestBuildSnoozeExpires(t *testing.T) {
	rep := report(grp("recurs twice", 2))
	led := NewMemLedger()
	now := time.Now()
	p := Build(rep, led, now)[0]
	if err := led.Snooze(Key(p.Target, p.Edit), now.Add(DefaultSnooze), now); err != nil {
		t.Fatalf("snooze: %v", err)
	}
	if got := Build(rep, led, now); len(got) != 0 {
		t.Fatalf("snoozed proposal should be hidden, got %d", len(got))
	}
	later := now.Add(DefaultSnooze + time.Hour)
	if got := Build(rep, led, later); len(got) != 1 {
		t.Fatalf("snooze should expire and re-offer, got %d", len(got))
	}
}

func TestKeySupersededWhenEditChanges(t *testing.T) {
	led := NewMemLedger()
	now := time.Now()
	if err := led.Dismiss(Key("AGENT.md", "old line"), now); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	// A refined edit keys differently, so the prior dismissal does not suppress it.
	if led.Suppressed(Key("AGENT.md", "refined line"), now) {
		t.Fatal("a superseding edit must not inherit the old dismissal")
	}
	if !led.Suppressed(Key("AGENT.md", "old line"), now) {
		t.Fatal("the identical edit must stay dismissed")
	}
}

func TestLedgerPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "improve-ledger.json")
	led, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()
	key := Key("AGENT.md", "remembered")
	if err := led.Dismiss(key, now); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	reopened, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !reopened.Suppressed(key, now) {
		t.Fatal("dismissal did not survive a reopen")
	}
}

func TestOpenLedgerMissingFileIsEmpty(t *testing.T) {
	led, err := OpenLedger(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if led.Suppressed(Key("AGENT.md", "x"), time.Now()) {
		t.Fatal("empty ledger should suppress nothing")
	}
}

func TestRenderListsProposalsAndEmpty(t *testing.T) {
	empty := Render(nil)
	if !strings.Contains(empty, "No proposals yet") {
		t.Fatalf("empty render missing guidance: %q", empty)
	}
	rep := report(skillrollup.Group{
		Kind: "fact", Summary: "pin the test command", Sessions: 4,
		Target: "AGENT.md", Diff: "- run: make test", Examples: []string{"s1", "s2"},
	})
	out := Render(Build(rep, NewMemLedger(), time.Now()))
	for _, want := range []string{"pin the test command", "AGENT.md", "make test", "4 sessions", "apply: /improve apply 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}
