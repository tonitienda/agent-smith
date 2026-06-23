// Package run is the durable queue and run bookkeeping for the background/async
// runner (AS-054). A "run" is a one-shot, unattended task: a prompt, an optional
// budget ceiling, and the record of how it turned out. The queue lives under the
// Smith data directory next to sessions, project-scoped the same way (AS-007), and
// records immutable run IDs so a run is auditable after the fact.
//
// The package owns persistence only — it never executes a run. The worker
// (cmd/smith `runs work`) drives execution through the same headless path a
// `smith run` uses (AS-051) and writes the outcome back here. Keeping the store
// stdlib-only and execution-free keeps it offline-testable and free of the
// provider/loop wiring (PRD D6, AS-095).
package run

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

	"github.com/tonitienda/agent-smith/schema"
)

// Status is the lifecycle state of a queued run. Statuses are additive (PRD D2):
// a reader that does not recognize one treats it as opaque rather than failing.
type Status string

const (
	// StatusQueued is enqueued and waiting for a worker.
	StatusQueued Status = "queued"
	// StatusRunning is claimed by a worker and executing.
	StatusRunning Status = "running"
	// StatusDone finished cleanly.
	StatusDone Status = "done"
	// StatusFailed ended in a permission denial, provider error, or internal error.
	StatusFailed Status = "failed"
	// StatusBudget halted at its budget ceiling (AS-041).
	StatusBudget Status = "budget"
	// StatusCanceled was canceled before it could finish (not resumable as-is).
	StatusCanceled Status = "canceled"
	// StatusInterrupted is a run whose worker died mid-flight; `runs resume`
	// re-queues it (AS-054 clarified decision: manual resume, no auto-restart).
	StatusInterrupted Status = "interrupted"
)

// Terminal reports whether a status is a finished outcome (no longer eligible for
// a worker to pick up without an explicit resume).
func (s Status) Terminal() bool {
	switch s {
	case StatusDone, StatusFailed, StatusBudget, StatusCanceled, StatusInterrupted:
		return true
	default:
		return false
	}
}

// Record is one run's durable bookkeeping. Outcome fields are zero until a worker
// finishes the run; new fields are additive (PRD D2) so an older record loads
// against a newer binary.
//
// There is deliberately no retry counter here: transient provider/network
// failures are retried *within* a turn by the loop's backoff policy (AS-018), so
// a run that still ends in a provider error has already exhausted those retries —
// re-running the whole task would only double-spend the budget and fork a second
// session. Persistent failures are recorded; `runs resume` is the manual retry.
type Record struct {
	ID          string  `json:"id"`
	ProjectPath string  `json:"project_path"`
	Prompt      string  `json:"prompt"`
	BudgetUSD   float64 `json:"budget_usd,omitempty"`
	Auto        bool    `json:"auto,omitempty"`
	Status      Status  `json:"status"`

	SessionID  string  `json:"session_id,omitempty"`
	StopReason string  `json:"stop_reason,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	ExitCode   int     `json:"exit_code,omitempty"`
	Error      string  `json:"error,omitempty"`

	// WorkerID and HeartbeatAt are the concurrency-worker lease (AS-132): a worker
	// stamps its id and a periodically-refreshed timestamp on a run it is executing
	// so peers can tell a live run from one whose worker crashed. Both are additive
	// (PRD D2) — a single-worker record simply never sets them.
	WorkerID    string     `json:"worker_id,omitempty"`
	HeartbeatAt *time.Time `json:"heartbeat_at,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Spec is the request to enqueue a run. It carries only what the caller chooses;
// the store fills in identity, project, and timestamps.
type Spec struct {
	Prompt    string
	BudgetUSD float64
	Auto      bool
}

const (
	recordFile = "run.json"
	// leaseFile is the per-run atomic claim gate for concurrent workers (AS-132).
	// Its existence — created with O_EXCL — means a worker owns (or owned) the run;
	// liveness is tracked separately by Record.HeartbeatAt so a crashed owner's run
	// can be reclaimed.
	leaseFile = "lease"
)

// Store owns the run queue directory for one project. The zero value is invalid;
// use NewStore so project paths are normalized before hashing (matching the
// session store layout so runs sit alongside the sessions they create).
type Store struct {
	root       string
	projectDir string
	projectKey string
}

// NewStore returns a run store rooted at root/runs/<project-hash>. If root is
// empty, DefaultRoot is used. projectDir is converted to an absolute, cleaned
// path before hashing so relative invocations from the same project share a queue.
func NewStore(root, projectDir string) (*Store, error) {
	if root == "" {
		root = DefaultRoot()
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("run: resolve project dir: %w", err)
	}
	abs = filepath.Clean(abs)
	return &Store{root: root, projectDir: abs, projectKey: projectHash(abs)}, nil
}

// DefaultRoot is the conventional Agent Smith state directory, shared with the
// session store so runs and sessions live under one home.
func DefaultRoot() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".agent-smith")
	}
	return ".agent-smith"
}

// ProjectPath returns the normalized project directory this store scopes.
func (s *Store) ProjectPath() string { return s.projectDir }

// RunsDir returns the directory holding this project's run records.
func (s *Store) RunsDir() string { return filepath.Join(s.root, "runs", s.projectKey) }

// Enqueue writes a new queued run record and returns it. The record is fsynced
// before returning so a crash right after enqueue does not lose the request.
func (s *Store) Enqueue(spec Spec) (Record, error) {
	if strings.TrimSpace(spec.Prompt) == "" {
		return Record{}, errors.New("run: empty prompt")
	}
	rec := Record{
		ID:          newID(),
		ProjectPath: s.projectDir,
		Prompt:      spec.Prompt,
		BudgetUSD:   spec.BudgetUSD,
		Auto:        spec.Auto,
		Status:      StatusQueued,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Save(rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Get loads a single run record by ID.
func (s *Store) Get(id string) (Record, error) {
	if !safeID(id) {
		return Record{}, fmt.Errorf("run: unsafe run id %q", id)
	}
	return readRecord(filepath.Join(s.RunsDir(), id, recordFile))
}

// List returns every run for this project, newest first by creation time.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.RunsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("run: list runs: %w", err)
	}
	var out []Record
	for _, e := range entries {
		if !e.IsDir() || !safeID(e.Name()) {
			continue
		}
		// The runs directory is already uniquely scoped to this project by the
		// SHA-256 projectKey, so no secondary ProjectPath comparison is needed — and
		// a strict string compare would fragilely skip valid runs when the project is
		// reached via a different symlink/case path than the one on the record.
		rec, err := readRecord(filepath.Join(s.RunsDir(), e.Name(), recordFile))
		if err != nil {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Save atomically writes a run record (temp file + rename + dir fsync), so a
// concurrent reader never sees a half-written record and a crash mid-write leaves
// the prior record intact.
func (s *Store) Save(rec Record) error {
	if !safeID(rec.ID) {
		return fmt.Errorf("run: unsafe run id %q", rec.ID)
	}
	dir := filepath.Join(s.RunsDir(), rec.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("run: create dir: %w", err)
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("run: marshal record: %w", err)
	}
	b = append(b, '\n')
	return writeFileSync(filepath.Join(dir, recordFile), b)
}

// Claim atomically transitions the queued run id to running, owned by workerID.
// The claim gate is an O_EXCL lease file in the run's directory, so among any
// number of competing workers exactly one wins a given run; the rest get
// claimed=false (no error) and move on (AS-132). A worker that wins must heartbeat
// the run (Heartbeat) so a survivor can tell a live claim from a crashed one
// (Reclaim), and release it when done (Release).
func (s *Store) Claim(id, workerID string) (Record, bool, error) {
	if !safeID(id) {
		return Record{}, false, fmt.Errorf("run: unsafe run id %q", id)
	}
	lease := filepath.Join(s.RunsDir(), id, leaseFile)
	f, err := os.OpenFile(lease, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, os.ErrExist) {
		return Record{}, false, nil // another worker holds (or held) this run
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("run: acquire lease: %w", err)
	}
	_, _ = f.WriteString(workerID)
	_ = f.Close()

	rec, err := s.Get(id)
	if err != nil {
		_ = os.Remove(lease)
		return Record{}, false, err
	}
	// We won the lease, but only a still-queued run is ours to start; anything else
	// (already finished, or a stray lingering lease) is left alone.
	if rec.Status != StatusQueued {
		_ = os.Remove(lease)
		return Record{}, false, nil
	}
	now := time.Now().UTC()
	rec.Status = StatusRunning
	rec.WorkerID = workerID
	rec.StartedAt = &now
	rec.HeartbeatAt = &now
	if err := s.Save(rec); err != nil {
		_ = os.Remove(lease)
		return Record{}, false, err
	}
	return rec, true, nil
}

// Heartbeat refreshes the liveness timestamp on a run this worker owns. It is a
// best-effort no-op when the run is no longer ours or no longer running (a final
// Save raced ahead), so a late heartbeat never resurrects a finished run.
func (s *Store) Heartbeat(id, workerID string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	if rec.Status != StatusRunning || rec.WorkerID != workerID {
		return nil
	}
	now := time.Now().UTC()
	rec.HeartbeatAt = &now
	return s.Save(rec)
}

// Release removes a run's lease so a later `runs resume` can re-claim it. A
// terminal run keeps no lease; a missing lease is not an error.
func (s *Store) Release(id string) error {
	if !safeID(id) {
		return fmt.Errorf("run: unsafe run id %q", id)
	}
	if err := os.Remove(filepath.Join(s.RunsDir(), id, leaseFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("run: release lease: %w", err)
	}
	return nil
}

// Reclaim marks every stale `running` run — a worker that died mid-flight — as
// interrupted and clears its lease, so a survivor never collides with it and the
// run becomes eligible for `runs resume` (AS-132). A run is stale when its
// heartbeat is missing or older than staleAfter; a live worker heartbeats well
// inside that window, so its in-flight run is left untouched (safe to run even as
// peers start up). Returns the reclaimed records.
func (s *Store) Reclaim(staleAfter time.Duration, now time.Time) ([]Record, error) {
	recs, err := s.List()
	if err != nil {
		return nil, err
	}
	var reclaimed []Record
	for _, rec := range recs {
		if rec.Status != StatusRunning {
			continue
		}
		if rec.HeartbeatAt != nil && now.Sub(*rec.HeartbeatAt) <= staleAfter {
			continue // a live worker is heartbeating this run
		}
		rec.Status = StatusInterrupted
		if rec.Error == "" {
			rec.Error = "worker exited before the run finished"
		}
		if err := s.Save(rec); err != nil {
			return reclaimed, err
		}
		if err := s.Release(rec.ID); err != nil {
			return reclaimed, err
		}
		reclaimed = append(reclaimed, rec)
	}
	return reclaimed, nil
}

func readRecord(path string) (Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("run: read record: %w", err)
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return Record{}, fmt.Errorf("run: parse record: %w", err)
	}
	return rec, nil
}

// writeFileSync writes b to path atomically: a sibling temp file is written and
// fsynced, renamed over path, then the directory is fsynced so the rename is
// durable.
func writeFileSync(path string, b []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("run: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("run: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("run: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("run: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("run: commit record: %w", err)
	}
	cleanup = false
	return syncDirBestEffort(dir)
}

func syncDirBestEffort(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("run: open dir for sync: %w", err)
	}
	// A directory fsync is unsupported or behaves differently on several
	// filesystems (NFS, WSL, virtual FSes) and may return ENOTSUP/EINVAL. The
	// record file is already written, synced, and renamed, so the data is durable;
	// treat the dir sync as strictly best-effort rather than failing Save.
	_ = d.Sync()
	if err := d.Close(); err != nil {
		return fmt.Errorf("run: close dir after sync: %w", err)
	}
	return nil
}

func projectHash(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return hex.EncodeToString(sum[:])[:16]
}

func newID() string {
	return "run_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + schema.NewID()
}

func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}
