package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// capture is a tiny helper: snapshot the current state of abs before writing
// post, then perform the write, mirroring what the write/edit tools do. The
// store reads abs itself for the pre-state, so this captures before the write.
func capture(t *testing.T, s *Store, id, rel, abs, post string) {
	t.Helper()
	if err := s.Capture(id, rel, abs, []byte(post)); err != nil {
		t.Fatalf("capture %s: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte(post), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// TestRestoreExistingFile: a file edited after the checkpoint is restored to its
// pre-checkpoint content (AC2).
func TestRestoreExistingFile(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "a.txt")
	if err := os.WriteFile(abs, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(proj, ".snap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	capture(t, s, "tu1", "a.txt", abs, "v1") // edit at/after checkpoint
	capture(t, s, "tu2", "a.txt", abs, "v2") // a later edit, same file

	muts := []Mutation{{ToolUseID: "tu1", Seq: 1}, {ToolUseID: "tu2", Seq: 2}}
	actions := s.PlanRestore(muts)
	if len(actions) != 1 || actions[0].Kind != ActionRestore {
		t.Fatalf("want one restore action, got %+v", actions)
	}
	res, err := s.ApplyRestore(actions)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := mustRead(t, abs); got != "v0" {
		t.Fatalf("restored content = %q, want %q", got, "v0")
	}
	if len(res.Restored) != 1 || res.Restored[0] != "a.txt" {
		t.Fatalf("result = %+v", res)
	}
}

// TestRestoreDeletesNewFile: a file created after the checkpoint is removed on
// restore, since it did not exist at the checkpoint.
func TestRestoreDeletesNewFile(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "new.txt")
	s, err := Open(filepath.Join(proj, ".snap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	capture(t, s, "tu1", "new.txt", abs, "created") // file did not exist before

	actions := s.PlanRestore([]Mutation{{ToolUseID: "tu1", Seq: 1}})
	if len(actions) != 1 || actions[0].Kind != ActionDelete {
		t.Fatalf("want delete action, got %+v", actions)
	}
	if _, err := s.ApplyRestore(actions); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("file should have been deleted, stat err = %v", err)
	}
}

// TestCreatedThenExternallyDeleted: a file Smith created after the checkpoint
// that is already gone from disk is a no-op delete, not a conflict — restoring
// to "absent" is exactly the on-disk state.
func TestCreatedThenExternallyDeleted(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "gone.txt")
	s, err := Open(filepath.Join(proj, ".snap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	capture(t, s, "tu1", "gone.txt", abs, "created") // new file
	if err := os.Remove(abs); err != nil {           // deleted outside Smith
		t.Fatal(err)
	}

	actions := s.PlanRestore([]Mutation{{ToolUseID: "tu1", Seq: 1}})
	if len(actions) != 1 || actions[0].Kind != ActionDelete {
		t.Fatalf("want delete (no-op) action, got %+v", actions)
	}
	if _, err := s.ApplyRestore(actions); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

// TestConflictDetected: a file changed outside Smith since the snapshot is
// flagged as a conflict and never overwritten (AC3, §6 no-data-loss).
func TestConflictDetected(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "c.txt")
	if err := os.WriteFile(abs, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(proj, ".snap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	capture(t, s, "tu1", "c.txt", abs, "smith-wrote")
	// A hand edit after Smith's write: the on-disk content no longer matches what
	// Smith last wrote.
	if err := os.WriteFile(abs, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	actions := s.PlanRestore([]Mutation{{ToolUseID: "tu1", Seq: 1}})
	if len(actions) != 1 || actions[0].Kind != ActionConflict {
		t.Fatalf("want conflict action, got %+v", actions)
	}
	res, err := s.ApplyRestore(actions)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := mustRead(t, abs); got != "hand-edited" {
		t.Fatalf("conflicted file was modified: %q", got)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("want one conflict, got %+v", res)
	}
}

// TestLargeFileSkipped: a file exceeding the size cap is recorded as skipped and
// cannot be restored (AC4).
func TestLargeFileSkipped(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "big.txt")
	if err := os.WriteFile(abs, []byte("small-before"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(proj, ".snap"), WithMaxBytes(8))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	capture(t, s, "tu1", "big.txt", abs, "a-post-content-well-over-eight-bytes")

	rec, ok := s.Lookup("tu1")
	if !ok || !rec.Skipped {
		t.Fatalf("record should be skipped: %+v ok=%v", rec, ok)
	}
	actions := s.PlanRestore([]Mutation{{ToolUseID: "tu1", Seq: 1}})
	if len(actions) != 1 || actions[0].Kind != ActionLargeSkip {
		t.Fatalf("want large-skip action, got %+v", actions)
	}
}

// TestMissingSnapshotOmitted: a mutation with no captured snapshot is omitted
// from the plan, so the caller can report it as not restorable.
func TestMissingSnapshotOmitted(t *testing.T) {
	proj := t.TempDir()
	s, err := Open(filepath.Join(proj, ".snap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if got := s.PlanRestore([]Mutation{{ToolUseID: "unknown", Seq: 1}}); len(got) != 0 {
		t.Fatalf("want no actions for an unknown mutation, got %+v", got)
	}
}

// TestPersistenceReload: a reopened store can still restore files captured in an
// earlier run (snapshots survive /resume).
func TestPersistenceReload(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "p.txt")
	if err := os.WriteFile(abs, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(proj, ".snap")

	s1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	capture(t, s1, "tu1", "p.txt", abs, "v1")
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	rec, ok := s2.Lookup("tu1")
	if !ok || rec.PreHash == "" {
		t.Fatalf("record not reloaded: %+v ok=%v", rec, ok)
	}
	actions := s2.PlanRestore([]Mutation{{ToolUseID: "tu1", Seq: 1}})
	if _, err := s2.ApplyRestore(actions); err != nil {
		t.Fatalf("apply after reload: %v", err)
	}
	if got := mustRead(t, abs); got != "v0" {
		t.Fatalf("restored content after reload = %q, want v0", got)
	}
}

// TestEarliestPreStateRestored: with several edits to one file, restore returns
// the earliest pre-state, not the most recent (the checkpoint state).
func TestEarliestPreStateRestored(t *testing.T) {
	proj := t.TempDir()
	abs := filepath.Join(proj, "m.txt")
	if err := os.WriteFile(abs, []byte("checkpoint"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(proj, ".snap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	capture(t, s, "tu1", "m.txt", abs, "step1")
	capture(t, s, "tu2", "m.txt", abs, "step2")
	capture(t, s, "tu3", "m.txt", abs, "step3")

	// Pass mutations out of order to prove the plan sorts by Seq.
	muts := []Mutation{{ToolUseID: "tu3", Seq: 3}, {ToolUseID: "tu1", Seq: 1}, {ToolUseID: "tu2", Seq: 2}}
	if _, err := s.ApplyRestore(s.PlanRestore(muts)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := mustRead(t, abs); got != "checkpoint" {
		t.Fatalf("restored = %q, want checkpoint", got)
	}
}
