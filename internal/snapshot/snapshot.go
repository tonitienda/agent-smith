// Package snapshot captures the pre-mutation contents of files the built-in
// write/edit tools touch, so `/rewind --restore-files` (AS-084) can put the
// working tree back to the state it held at a checkpoint. AS-037 rewinds the
// conversation only and merely *warns* about files changed afterwards; this
// store is the mechanism that lets a rewind also restore those files.
//
// A snapshot is keyed by the tool call's tool_use id, which uniquely identifies
// the write/edit on the event log — so restore correlates a dropped tool_call
// block to the content that file held just before that call, independent of how
// the calls were ordered or run (AS-019 runs a turn's calls concurrently). The
// pre-content is stored content-addressed under the session data directory and
// referenced from an append-only manifest (additive-only, like the rest of our
// on-disk formats, D2). Capture honours the no-data-loss guardrail (§6) at
// restore time: a file changed outside Smith since the snapshot is flagged as a
// conflict and never silently clobbered.
package snapshot

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// DefaultMaxBytes caps the file size the store will snapshot. A file whose pre-
// or post-content exceeds it is recorded as skipped (it cannot be restored) and
// reported in the rewind preview, so a huge file never bloats the session
// directory (AS-084 storage decision).
const DefaultMaxBytes = 5 << 20 // 5 MiB

// Record is one captured mutation: the file a write/edit touched, keyed by its
// tool_use id. PreHash names the content-addressed object holding the file's
// content before the mutation (empty when the file did not exist — restore then
// means delete). PostHash is the hash of the content the tool wrote, used to
// detect external modification at restore time. Skipped marks a file too large
// to snapshot.
type Record struct {
	ToolUseID string `json:"tool_use_id"`
	Path      string `json:"path"`     // project-relative, for display
	AbsPath   string `json:"abs_path"` // absolute, for restore
	PreHash   string `json:"pre_hash,omitempty"`
	PostHash  string `json:"post_hash,omitempty"`
	PreExists bool   `json:"pre_exists"`
	Mode      uint32 `json:"mode,omitempty"` // file perms to restore with
	Skipped   bool   `json:"skipped,omitempty"`
	Size      int    `json:"size,omitempty"` // larger of pre/post, for the skip message
}

// Store is the per-session snapshot store. It is safe for concurrent use: the
// runtime may run several file tool calls against it at once (AS-019).
type Store struct {
	mu       sync.Mutex
	dir      string
	objects  string
	maxBytes int
	recs     map[string]Record // tool_use id -> record
	mf       *os.File          // append-only manifest handle
}

// Option configures a Store.
type Option func(*Store)

// WithMaxBytes overrides the size cap above which files are skipped rather than
// snapshotted. A non-positive n is ignored.
func WithMaxBytes(n int) Option {
	return func(s *Store) {
		if n > 0 {
			s.maxBytes = n
		}
	}
}

// Open creates (or reopens) a snapshot store rooted at dir, replaying any
// existing manifest so a resumed session can still restore files captured in an
// earlier run. The manifest is then positioned for appends.
func Open(dir string, opts ...Option) (*Store, error) {
	s := &Store{
		dir:      dir,
		objects:  filepath.Join(dir, "objects"),
		maxBytes: DefaultMaxBytes,
		recs:     map[string]Record{},
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := os.MkdirAll(s.objects, 0o755); err != nil {
		return nil, fmt.Errorf("snapshot: create dir: %w", err)
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.manifestPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("snapshot: open manifest: %w", err)
	}
	s.mf = f
	return s, nil
}

func (s *Store) manifestPath() string { return filepath.Join(s.dir, "manifest.jsonl") }

// load replays the manifest into the in-memory index. A torn final line (a crash
// mid-append) is tolerated and skipped rather than failing the whole load.
func (s *Store) load() error {
	f, err := os.Open(s.manifestPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("snapshot: read manifest: %w", err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // tolerate a torn final line
		}
		if r.ToolUseID != "" {
			s.recs[r.ToolUseID] = r
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("snapshot: scan manifest: %w", err)
	}
	return nil
}

// Capture records the pre-mutation state of the file at absPath about to be
// written by a write/edit call identified by toolUseID; post is the content the
// tool is about to write. The Store reads the file's current content itself,
// guarded by a stat-then-size check so a very large file (a big log or SQL dump
// the agent overwrites) is recorded as skipped without ever being read into
// memory — it cannot be restored, but the rewind preview reports the skip. It is
// best-effort: an empty toolUseID is a no-op. The returned error is for
// tests/callers that care; the file tools ignore it so a snapshot failure never
// aborts the write.
func (s *Store) Capture(toolUseID, relPath, absPath string, post []byte) error {
	if toolUseID == "" {
		return nil
	}
	rec := Record{ToolUseID: toolUseID, Path: relPath, AbsPath: absPath}

	info, statErr := os.Stat(absPath)
	preExists := statErr == nil && !info.IsDir()
	rec.PreExists = preExists
	var preSize int64
	if preExists {
		preSize = info.Size()
		rec.Mode = uint32(info.Mode().Perm())
	}

	// Decide skip from the on-disk size, never from bytes read into memory.
	if preSize > int64(s.maxBytes) || len(post) > s.maxBytes {
		rec.Skipped = true
		rec.PreExists = preExists
		rec.Size = int(max(preSize, int64(len(post))))
		return s.append(rec)
	}

	rec.PostHash = hashBytes(post)
	if preExists {
		pre, err := os.ReadFile(absPath)
		if err != nil {
			// The file vanished between stat and read (a race): record no
			// pre-state rather than fail the caller's write.
			rec.PreExists = false
		} else {
			rec.PreHash = hashBytes(pre)
			if err := s.writeObject(rec.PreHash, pre); err != nil {
				return err
			}
		}
	}
	return s.append(rec)
}

// Lookup returns the record captured for a tool_use id, if any.
func (s *Store) Lookup(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[id]
	return r, ok
}

// Close releases the manifest file handle.
func (s *Store) Close() error {
	if s == nil || s.mf == nil {
		return nil
	}
	return s.mf.Close()
}

// append records r in the manifest (durably) and the in-memory index under lock.
func (s *Store) append(r Record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("snapshot: marshal record: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.mf.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("snapshot: write manifest: %w", err)
	}
	s.recs[r.ToolUseID] = r
	return nil
}

// writeObject stores data under its hash, deduplicating: an object already
// present (a file written with the same content before) is left as is. The write
// is atomic — a sibling temp file renamed into place — so an interrupted write
// (crash, power loss, disk full) never leaves a partial object that the Stat
// dedup check would later mistake for a complete one. Two concurrent tool calls
// writing the same hash race only on the final rename, which is safe: both hold
// identical bytes.
func (s *Store) writeObject(hash string, data []byte) error {
	p := filepath.Join(s.objects, hash)
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(s.objects, ".tmp-*")
	if err != nil {
		return fmt.Errorf("snapshot: create temp object: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("snapshot: write temp object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("snapshot: close temp object: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("snapshot: rename object: %w", err)
	}
	return nil
}

func (s *Store) readObject(hash string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.objects, hash))
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sortRecords orders records by ascending event sequence.
func sortBySeq(es []seqRecord) {
	sort.Slice(es, func(i, j int) bool { return es[i].seq < es[j].seq })
}
