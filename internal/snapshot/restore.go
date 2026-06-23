package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Mutation identifies one dropped write/edit on the event log: the call's
// tool_use id (the snapshot key) and the block's sequence (so the store can
// order several mutations to the same file and restore the earliest pre-state).
type Mutation struct {
	ToolUseID string
	Seq       int
}

// ActionKind is what a restore would do to one file.
type ActionKind int

const (
	// ActionRestore rewrites the file with its pre-checkpoint content.
	ActionRestore ActionKind = iota
	// ActionDelete removes a file that did not exist at the checkpoint.
	ActionDelete
	// ActionConflict means the file changed outside Smith since the snapshot; the
	// restore refuses to touch it (no-data-loss guardrail, §6).
	ActionConflict
	// ActionLargeSkip means the file was too large to snapshot, so it cannot be
	// restored.
	ActionLargeSkip
)

// FileAction is the planned restore for one file. preHash and mode are internal
// to ApplyRestore; callers render Path and Kind.
type FileAction struct {
	Path    string
	AbsPath string
	Kind    ActionKind

	preHash string
	mode    os.FileMode
}

// Result reports what ApplyRestore did, by project-relative path.
type Result struct {
	Restored  []string
	Deleted   []string
	Conflicts []string
	Skipped   []string
}

type seqRecord struct {
	seq int
	rec Record
}

// PlanRestore computes, for each file the dropped mutations touched, how to put
// it back to its pre-checkpoint state. For a file with several dropped
// mutations, the earliest pre-state is the checkpoint state and the latest
// post-state is what Smith last wrote — compared against the file on disk to
// detect external modification. Mutations whose snapshot is missing (captured
// before this store existed, or a capture that failed) are simply omitted; the
// caller reconciles them against the rewind's modified-files list. The plan
// mutates nothing.
func (s *Store) PlanRestore(muts []Mutation) []FileAction {
	byPath := map[string][]seqRecord{}
	for _, m := range muts {
		rec, ok := s.Lookup(m.ToolUseID)
		if !ok {
			continue // no snapshot for this call; reconciled by the caller
		}
		byPath[rec.AbsPath] = append(byPath[rec.AbsPath], seqRecord{seq: m.Seq, rec: rec})
	}

	actions := make([]FileAction, 0, len(byPath))
	for abs, es := range byPath {
		sortBySeq(es)
		earliest, latest := es[0].rec, es[len(es)-1].rec
		fa := FileAction{Path: earliest.Path, AbsPath: abs, mode: os.FileMode(earliest.Mode)}
		matches, exists := diskMatches(abs, latest)
		switch {
		case earliest.Skipped || latest.Skipped:
			fa.Kind = ActionLargeSkip
		case !earliest.PreExists && !exists:
			// A file Smith created that is already gone: restoring to "absent" is a
			// no-op, not a conflict (it only looks like a mismatch because the read
			// failed with ErrNotExist).
			fa.Kind = ActionDelete
		case !matches:
			fa.Kind = ActionConflict
		case !earliest.PreExists:
			fa.Kind = ActionDelete
		default:
			fa.Kind = ActionRestore
			fa.preHash = earliest.PreHash
		}
		actions = append(actions, fa)
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Path < actions[j].Path })
	return actions
}

// ApplyRestore executes a restore plan: it rewrites or deletes files whose state
// is unambiguous and leaves conflicted or too-large files untouched, reporting
// every outcome. Recompute the plan immediately before applying so conflict
// detection reflects the current working tree.
func (s *Store) ApplyRestore(actions []FileAction) (Result, error) {
	var res Result
	for _, a := range actions {
		switch a.Kind {
		case ActionRestore:
			data, err := s.readObject(a.preHash)
			if err != nil {
				return res, fmt.Errorf("snapshot: read object for %s: %w", a.Path, err)
			}
			if err := os.MkdirAll(filepath.Dir(a.AbsPath), 0o755); err != nil {
				return res, fmt.Errorf("snapshot: mkdir for %s: %w", a.Path, err)
			}
			mode := a.mode
			if mode == 0 {
				mode = 0o644
			}
			if err := atomicWrite(a.AbsPath, data, mode); err != nil {
				return res, fmt.Errorf("snapshot: restore %s: %w", a.Path, err)
			}
			res.Restored = append(res.Restored, a.Path)
		case ActionDelete:
			if err := os.Remove(a.AbsPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return res, fmt.Errorf("snapshot: delete %s: %w", a.Path, err)
			}
			res.Deleted = append(res.Deleted, a.Path)
		case ActionConflict:
			res.Conflicts = append(res.Conflicts, a.Path)
		case ActionLargeSkip:
			res.Skipped = append(res.Skipped, a.Path)
		}
	}
	return res, nil
}

// diskMatches reports whether the file at abs currently holds the content Smith
// last wrote (latest.PostHash) and whether it exists at all. The exists flag lets
// PlanRestore tell an externally-deleted file (a conflict) apart from a
// Smith-created file that is already gone (a no-op delete). A read error other
// than "not exist" counts as not-matching but still existing, so it is treated
// conservatively as a conflict.
func diskMatches(abs string, latest Record) (matches, exists bool) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return false, !errors.Is(err, fs.ErrNotExist)
	}
	return hashBytes(data) == latest.PostHash, true
}

// atomicWrite writes data to abs atomically via a sibling temp file and rename,
// so an interrupted restore (crash, power loss, disk full) leaves the file
// untouched rather than half-written.
func atomicWrite(abs string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".smith-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}
