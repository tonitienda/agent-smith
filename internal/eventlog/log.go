// Package eventlog implements the append-only, immutable event log that backs
// every Agent Smith session (AS-005, PRD D3). A session is recorded as an
// ordered sequence of content blocks (package schema); the model-facing context
// is a projection over this log (AS-006), never stored state.
//
// The public API offers append and read only. There is deliberately no update
// or delete operation, so history is immutable by construction: edits to the
// conversation (/clean, /compact, /rewind) are themselves appended as exclusion
// or derived-block events (see events.go) that drop or replace blocks in the
// projection without ever mutating or removing an earlier event. Reversibility
// and auditability are therefore structural.
//
// A Log may be purely in-memory (New) or disk-backed (Open). The disk format is
// append-only JSONL — one event per line. Appending a line is O(1) and
// crash-safe: a process killed mid-append never corrupts previously written
// events, and a torn trailing line is discarded on reload.
package eventlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/schema"
)

// Log is an in-memory, append-only event log of schema.Blocks, optionally
// written through to a disk-backed JSONL file. It is safe for concurrent use:
// appends are serialized and reads take a read lock.
type Log struct {
	mu      sync.RWMutex
	events  []schema.Block
	byID    map[string]int // block ID -> index in events
	nextSeq int

	// Disk backing (nil for an in-memory-only log).
	file   *os.File
	closed bool
}

// New returns an empty, in-memory-only log. Its events are never persisted; use
// Open for a disk-backed session.
func New() *Log {
	return &Log{byID: make(map[string]int)}
}

// Open opens the disk-backed log at path, creating the file if it does not
// exist, and replays any existing events into memory. Subsequent appends are
// written through to disk before Append returns, so an event that has been
// acknowledged is durable. A torn final line left by an earlier crash is
// discarded during replay; complete lines that fail to parse are reported as
// corruption. Close must be called to release the file.
func Open(path string) (*Log, error) {
	l := New()

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	if err := l.replay(f); err != nil {
		f.Close()
		return nil, err
	}
	// Position the file at the end so appends extend the log; replay leaves the
	// offset wherever the last complete line ended (which also truncates a torn
	// trailing fragment from the next write's perspective).
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("eventlog: seek %s: %w", path, err)
	}
	l.file = f
	return l, nil
}

// replay reads complete JSONL lines from f into the in-memory log. A trailing
// fragment without a newline is treated as a torn write from a crash and
// discarded; f is truncated to the end of the last complete line so it never
// gets concatenated with a future append into one malformed line.
func (l *Log) replay(f *os.File) error {
	r := bufio.NewReader(f)
	var offset int64
	for {
		line, err := r.ReadBytes('\n')
		// A non-EOF read error is a real I/O failure; surface it directly rather
		// than mistaking the partial bytes for JSON corruption.
		if err != nil && err != io.EOF {
			return fmt.Errorf("eventlog: read: %w", err)
		}
		if len(line) > 0 && err == io.EOF {
			// Unterminated trailing fragment: a torn write. Drop it and reclaim
			// the bytes so the next append starts a fresh, valid line.
			if terr := f.Truncate(offset); terr != nil {
				return fmt.Errorf("eventlog: truncate torn tail: %w", terr)
			}
			break
		}
		if len(line) > 0 {
			var b schema.Block
			if perr := json.Unmarshal(line, &b); perr != nil {
				return fmt.Errorf("eventlog: corrupt event at offset %d: %w", offset, perr)
			}
			if aerr := l.appendReplayed(b); aerr != nil {
				return aerr
			}
			offset += int64(len(line))
		}
		if err == io.EOF {
			break
		}
	}
	return nil
}

// appendReplayed inserts a block read from disk, preserving its stored Seq and
// keeping nextSeq ahead of every seen Seq so future appends stay monotonic.
func (l *Log) appendReplayed(b schema.Block) error {
	if err := b.Validate(); err != nil {
		return fmt.Errorf("eventlog: replayed %w", err)
	}
	if _, dup := l.byID[b.ID]; dup {
		return fmt.Errorf("eventlog: duplicate block id %q on reload", b.ID)
	}
	l.byID[b.ID] = len(l.events)
	l.events = append(l.events, b)
	if b.Seq >= l.nextSeq {
		l.nextSeq = b.Seq + 1
	}
	return nil
}

// Append validates b, assigns its monotonic Seq (and an append timestamp if it
// has none), persists it when the log is disk-backed, and adds it to the log.
// It returns the stored block. Append never updates or removes an existing
// event; a block whose ID is already present is rejected, because IDs are
// unique and never reused.
func (l *Log) Append(b schema.Block) (schema.Block, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return schema.Block{}, errors.New("eventlog: append to a closed log")
	}
	if b.ID == "" {
		return schema.Block{}, errors.New("eventlog: cannot append a block with no id")
	}
	if _, dup := l.byID[b.ID]; dup {
		return schema.Block{}, fmt.Errorf("eventlog: block id %q already on the log", b.ID)
	}

	b.Seq = l.nextSeq
	if b.TS.IsZero() {
		// The append timestamp is the harness clock at append time. UTC() also
		// strips the monotonic reading so the value round-trips through JSON
		// unchanged.
		b.TS = time.Now().UTC()
	}
	if err := b.Validate(); err != nil {
		return schema.Block{}, fmt.Errorf("eventlog: %w", err)
	}

	// Persist before mutating in-memory state: if the write fails the log is
	// unchanged and the error is reported, so memory and disk never diverge.
	if l.file != nil {
		if err := l.persist(b); err != nil {
			return schema.Block{}, err
		}
	}

	l.byID[b.ID] = len(l.events)
	l.events = append(l.events, b)
	l.nextSeq++
	return b, nil
}

// persist writes one event as a single JSONL line and fsyncs it so a crash
// after Append returns cannot lose it. The line is written with one Write call
// to the file directly — no buffering — so a failed write leaves no half-line
// queued to be re-emitted on the next append (which would corrupt the log).
func (l *Log) persist(b schema.Block) error {
	line, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("eventlog: marshal %s: %w", b.ID, err)
	}
	line = append(line, '\n')
	if _, err := l.file.Write(line); err != nil {
		return fmt.Errorf("eventlog: write %s: %w", b.ID, err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("eventlog: sync %s: %w", b.ID, err)
	}
	return nil
}

// Len returns the number of events on the log.
func (l *Log) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.events)
}

// At returns the event at index i in append order, and whether i is in range.
func (l *Log) At(i int) (schema.Block, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if i < 0 || i >= len(l.events) {
		return schema.Block{}, false
	}
	return l.events[i], true
}

// ByID returns the event with the given block ID, and whether it was found.
func (l *Log) ByID(id string) (schema.Block, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	i, ok := l.byID[id]
	if !ok {
		return schema.Block{}, false
	}
	return l.events[i], true
}

// Events returns a snapshot copy of every event in append order. Adding to or
// reordering the returned slice does not affect the log. The copy is shallow,
// though: each Block still shares its pointer fields (Text, Provenance, …) with
// the stored event, so callers must treat the returned blocks as read-only.
func (l *Log) Events() []schema.Block {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]schema.Block, len(l.events))
	copy(out, l.events)
	return out
}

// Close marks the log closed — rejecting any further Append — and releases the
// disk file. It is safe to call more than once and is a no-op for an in-memory
// log beyond marking it closed.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	syncErr := l.file.Sync()
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(syncErr, closeErr)
}
