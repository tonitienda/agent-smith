// Package session persists Agent Smith sessions on disk (AS-007).
//
// A session is still just the append-only event log from internal/eventlog; this
// package gives logs a stable on-disk home, small human-readable metadata, and
// project-scoped discovery. Derived facts such as event counts, models used,
// byte size, and updated time are computed from the log when sessions are
// listed rather than duplicated into metadata.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

const (
	metadataFile = "metadata.json"
	eventLogFile = "events.jsonl"
)

// Store owns the session directory for one project. The zero value is invalid;
// use NewStore so project paths are normalized before hashing.
type Store struct {
	root       string
	projectDir string
	projectKey string
}

// Session is an opened, disk-backed session log plus its immutable metadata.
type Session struct {
	ID       string
	Dir      string
	Metadata Metadata
	Log      *eventlog.Log
}

// Metadata is the small file committed next to each event log. It contains only
// human/project identity fields that are not reconstructible from the event
// stream; runtime totals are deliberately absent and computed by List.
type Metadata struct {
	ID          string    `json:"id"`
	ProjectPath string    `json:"project_path"`
	CreatedAt   time.Time `json:"created_at"`
	Title       string    `json:"title,omitempty"`
}

// Summary is the project-scoped listing view for a stored session. Every field
// except Metadata is derived from the current event log or filesystem state.
type Summary struct {
	Metadata
	UpdatedAt  time.Time
	EventCount int
	SizeBytes  int64
	Models     []string
}

// NewStore returns a session store rooted at root/sessions/<project-hash>. If
// root is empty, DefaultRoot is used. projectDir is converted to an absolute,
// cleaned path before hashing so relative invocations from the same project are
// discoverable together.
func NewStore(root, projectDir string) (*Store, error) {
	if root == "" {
		root = DefaultRoot()
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("session: resolve project dir: %w", err)
	}
	absProject = filepath.Clean(absProject)
	return &Store{root: root, projectDir: absProject, projectKey: projectHash(absProject)}, nil
}

// DefaultRoot is the conventional Agent Smith state directory.
func DefaultRoot() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".agent-smith")
	}
	return ".agent-smith"
}

// ProjectDir returns the normalized project directory this store scopes.
func (s *Store) ProjectDir() string { return s.projectDir }

// ProjectSessionsDir returns the directory containing this project's sessions.
func (s *Store) ProjectSessionsDir() string {
	return filepath.Join(s.root, "sessions", s.projectKey)
}

// Create creates a new disk-backed session and opens its append-only log. The
// metadata file is fsynced before the session is returned, so a crash after
// Create does not leave an undiscoverable log directory.
func (s *Store) Create(title string) (*Session, error) {
	id := newID()
	dir := filepath.Join(s.ProjectSessionsDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("session: create dir: %w", err)
	}
	meta := Metadata{ID: id, ProjectPath: s.projectDir, CreatedAt: time.Now().UTC(), Title: title}
	if err := writeMetadata(dir, meta); err != nil {
		return nil, err
	}
	log, err := eventlog.Open(filepath.Join(dir, eventLogFile))
	if err != nil {
		return nil, err
	}
	return &Session{ID: id, Dir: dir, Metadata: meta, Log: log}, nil
}

// Open loads an existing project-scoped session by ID and replays its log.
func (s *Store) Open(id string) (*Session, error) {
	if !safeID(id) {
		return nil, fmt.Errorf("session: unsafe session id %q", id)
	}
	dir := filepath.Join(s.ProjectSessionsDir(), id)
	meta, err := readMetadata(dir)
	if err != nil {
		return nil, err
	}
	if meta.ID != id {
		return nil, fmt.Errorf("session: metadata id %q does not match directory id %q", meta.ID, id)
	}
	if meta.ProjectPath != s.projectDir {
		return nil, fmt.Errorf("session: %s belongs to project %q, not %q", id, meta.ProjectPath, s.projectDir)
	}
	log, err := eventlog.Open(filepath.Join(dir, eventLogFile))
	if err != nil {
		return nil, err
	}
	return &Session{ID: id, Dir: dir, Metadata: meta, Log: log}, nil
}

// List returns summaries for every session belonging to this store's project,
// newest first by derived UpdatedAt.
func (s *Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.ProjectSessionsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session: list project sessions: %w", err)
	}
	var out []Summary
	for _, e := range entries {
		if !e.IsDir() || !safeID(e.Name()) {
			continue
		}
		dir := filepath.Join(s.ProjectSessionsDir(), e.Name())
		meta, err := readMetadata(dir)
		if err != nil || meta.ProjectPath != s.projectDir {
			continue
		}
		summary, err := summarize(dir, meta)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func summarize(dir string, meta Metadata) (Summary, error) {
	logPath := filepath.Join(dir, eventLogFile)
	log, err := eventlog.Open(logPath)
	if err != nil {
		return Summary{}, err
	}
	events := log.Events()
	if err := log.Close(); err != nil {
		return Summary{}, err
	}
	stat, err := os.Stat(logPath)
	if err != nil {
		return Summary{}, fmt.Errorf("session: stat log: %w", err)
	}
	modelsSeen := make(map[string]bool)
	var models []string
	updated := meta.CreatedAt
	for _, b := range events {
		if b.TS.After(updated) {
			updated = b.TS
		}
		if b.Provider != nil && b.Provider.Model != "" && !modelsSeen[b.Provider.Model] {
			modelsSeen[b.Provider.Model] = true
			models = append(models, b.Provider.Model)
		}
	}
	sort.Strings(models)
	return Summary{Metadata: meta, UpdatedAt: updated, EventCount: len(events), SizeBytes: stat.Size(), Models: models}, nil
}

func writeMetadata(dir string, meta Metadata) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal metadata: %w", err)
	}
	b = append(b, '\n')
	path := filepath.Join(dir, metadataFile)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("session: write metadata: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("session: reopen metadata: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("session: sync metadata: %w", err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("session: open metadata directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("session: sync metadata directory: %w", err)
	}
	return nil
}

func readMetadata(dir string) (Metadata, error) {
	b, err := os.ReadFile(filepath.Join(dir, metadataFile))
	if err != nil {
		return Metadata{}, fmt.Errorf("session: read metadata: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(b, &meta); err != nil {
		return Metadata{}, fmt.Errorf("session: parse metadata: %w", err)
	}
	return meta, nil
}

func projectHash(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return hex.EncodeToString(sum[:])[:16]
}

func newID() string {
	return "sess_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + schema.NewID()
}

func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\\`)
}
