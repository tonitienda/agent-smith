// Package store is the orchestrator's SQLite run-control store (AS-161). It owns
// the *control* plane the daemon needs to schedule and supervise work: the loaded
// jobs and their triggers, the queue of runs, the per-run lease that makes
// claiming a run safe under concurrency, the attempt history behind retries, the
// idempotency keys that dedupe trigger fan-out, and an audit trail of operator and
// lifecycle actions.
//
// It deliberately holds **run-control state only** (ADR D-ORCH-4): the narrative
// of a run — model turns, tool calls, cost detail — stays in the normal Smith
// append-only session log (AS-005/007), read back through the existing readers
// (AS-151). The store records just enough about a run (status, failure class,
// session id, headline cost) to schedule, retry, gate, and report on it.
//
// SQLite is reached through the pure-Go modernc.org/sqlite driver so the single
// static `make build` binary keeps working (no cgo). The store serialises writes
// onto a single connection — a local orchestrator is a single-writer workload and
// this sidesteps SQLite's writer-contention corners without a second execution
// path.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// RunStatus is a run's lifecycle state. Statuses are additive (PRD D2): a reader
// that does not recognise one treats it as opaque rather than failing.
type RunStatus string

const (
	// StatusQueued is enqueued and waiting for a worker to claim it.
	StatusQueued RunStatus = "queued"
	// StatusRunning is claimed by a worker (holds a lease) and executing.
	StatusRunning RunStatus = "running"
	// StatusSucceeded finished cleanly.
	StatusSucceeded RunStatus = "succeeded"
	// StatusFailed ended in error; see FailureClass for the category.
	StatusFailed RunStatus = "failed"
	// StatusCanceled was canceled by an operator before it finished.
	StatusCanceled RunStatus = "canceled"
)

// Terminal reports whether a status is a finished outcome no longer eligible for
// a worker to claim.
func (s RunStatus) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

// FailureClass categorises why a run did not succeed so operators (and the failure
// hooks) can tell apart a missing credential from a blocked policy from a real
// internal error (AS-161 AC: "clearly distinguish missing permissions, missing
// secrets, invalid job specs, budget exhaustion, and blocked policy"). The empty
// value means "not a failure".
type FailureClass string

const (
	FailureNone              FailureClass = ""
	FailureMissingPermission FailureClass = "missing_permission"
	FailureMissingSecret     FailureClass = "missing_secret"
	FailureInvalidSpec       FailureClass = "invalid_spec"
	FailureBudgetExhausted   FailureClass = "budget_exhausted"
	FailureBlockedPolicy     FailureClass = "blocked_policy"
	FailureTimeout           FailureClass = "timeout"
	FailureInternal          FailureClass = "internal"
)

// Job is a loaded, validated job spec's control-plane row. The full typed spec is
// the loader's concern; the store keeps the identity, scope, and a paused flag the
// scheduler reads, plus the raw spec JSON so an operator can inspect what the
// daemon loaded without re-reading the working tree.
type Job struct {
	ID         string
	File       string
	Version    int
	Owner      string
	Repository string
	Org        string
	SpecJSON   string
	Paused     bool
	LoadedAt   time.Time
}

// NewRun is the request to enqueue a run. The store fills in identity and the
// queued timestamp; IdempotencyKey, when non-empty, makes Enqueue a no-op that
// returns the existing run if one was already enqueued under the same key.
type NewRun struct {
	JobID            string
	TriggerKind      string
	ConcurrencyKey   string
	ConcurrencyLimit int
	IdempotencyKey   string
	// TriggerContext is an opaque JSON blob the daemon stamps with the trigger's
	// context (e.g. a GitHub event's repository, issue/PR number, and actor) so a
	// deterministic hook can later target the right issue/PR without re-parsing the
	// original webhook. The store treats it as opaque bytes (AS-147).
	TriggerContext string
	BudgetUSD      float64
	Timeout        time.Duration
	MaxAttempts    int
}

// Run is one queued/active/finished run's control row.
type Run struct {
	ID               string
	JobID            string
	TriggerKind      string
	ConcurrencyKey   string
	ConcurrencyLimit int
	IdempotencyKey   string
	TriggerContext   string
	Status           RunStatus
	FailureClass     FailureClass
	Attempt          int
	MaxAttempts      int
	BudgetUSD        float64
	TimeoutSecs      int
	SessionID        string
	CostUSD          float64
	Error            string
	WorkerID         string
	QueuedAt         time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	HeartbeatAt      *time.Time
}

// Attempt records one execution attempt of a run (retry history).
type Attempt struct {
	RunID        string
	Number       int
	Status       RunStatus
	FailureClass FailureClass
	Error        string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

// AuditEntry is one operator- or lifecycle-action record.
type AuditEntry struct {
	At     time.Time
	JobID  string
	RunID  string
	Action string
	Detail string
}

// Store is the SQLite-backed run-control store. Use Open; the zero value is
// invalid. It is safe for concurrent use — writes serialise on a single
// connection.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the run-control database at path and applies the
// schema. Use ":memory:" for an ephemeral store in tests. The connection pool is
// capped at one so the single-writer SQLite workload never contends with itself.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("orchestrator/store: empty database path")
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("orchestrator/store: open %q: %w", path, err)
	}
	// One connection: SQLite is a single-writer engine and a local daemon is a
	// single-writer workload, so serialising here avoids "database is locked" without
	// WAL tuning, and keeps a ":memory:" store from fragmenting across connections.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// schema is the additive-only run-control schema (AS-161 AC). New columns/tables
// arrive additively (PRD D2); existing ones are never repurposed.
const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id          TEXT PRIMARY KEY,
	file        TEXT NOT NULL,
	version     INTEGER NOT NULL,
	owner       TEXT NOT NULL,
	repository  TEXT NOT NULL DEFAULT '',
	org         TEXT NOT NULL DEFAULT '',
	spec_json   TEXT NOT NULL DEFAULT '',
	paused      INTEGER NOT NULL DEFAULT 0,
	loaded_at   TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS triggers (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id    TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	kind      TEXT NOT NULL,
	args_json TEXT NOT NULL DEFAULT '',
	next_fire TIMESTAMP
);
CREATE INDEX IF NOT EXISTS triggers_job ON triggers(job_id);

CREATE TABLE IF NOT EXISTS runs (
	id                TEXT PRIMARY KEY,
	job_id            TEXT NOT NULL,
	trigger_kind      TEXT NOT NULL DEFAULT '',
	concurrency_key   TEXT NOT NULL DEFAULT '',
	concurrency_limit INTEGER NOT NULL DEFAULT 1,
	idempotency_key   TEXT NOT NULL DEFAULT '',
	trigger_context   TEXT NOT NULL DEFAULT '',
	status            TEXT NOT NULL,
	failure_class     TEXT NOT NULL DEFAULT '',
	attempt           INTEGER NOT NULL DEFAULT 0,
	max_attempts      INTEGER NOT NULL DEFAULT 1,
	budget_usd        REAL NOT NULL DEFAULT 0,
	timeout_secs      INTEGER NOT NULL DEFAULT 0,
	session_id        TEXT NOT NULL DEFAULT '',
	cost_usd          REAL NOT NULL DEFAULT 0,
	error             TEXT NOT NULL DEFAULT '',
	worker_id         TEXT NOT NULL DEFAULT '',
	queued_at         TIMESTAMP NOT NULL,
	started_at        TIMESTAMP,
	finished_at       TIMESTAMP,
	heartbeat_at      TIMESTAMP
);
CREATE INDEX IF NOT EXISTS runs_status ON runs(status, queued_at);
CREATE INDEX IF NOT EXISTS runs_concurrency ON runs(concurrency_key, status);

-- Idempotency keys dedupe trigger fan-out: the same delivered event maps to one
-- run. A NULL/empty key is never recorded, so unkeyed (e.g. manual) runs never
-- collide.
CREATE TABLE IF NOT EXISTS idempotency_keys (
	key        TEXT PRIMARY KEY,
	run_id     TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS attempts (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id        TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	number        INTEGER NOT NULL,
	status        TEXT NOT NULL,
	failure_class TEXT NOT NULL DEFAULT '',
	error         TEXT NOT NULL DEFAULT '',
	started_at    TIMESTAMP NOT NULL,
	finished_at   TIMESTAMP
);
CREATE INDEX IF NOT EXISTS attempts_run ON attempts(run_id);

CREATE TABLE IF NOT EXISTS audit (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	at     TIMESTAMP NOT NULL,
	job_id TEXT NOT NULL DEFAULT '',
	run_id TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT ''
);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("orchestrator/store: migrate: %w", err)
	}
	// Additively backfill columns onto stores created before they existed. The
	// schema DDL is create-if-not-exists, so it never touches an existing table;
	// an idempotent ADD COLUMN (ignoring the duplicate-column error) upgrades an
	// older runs table in place (D2 additive-only).
	if err := addColumn(s.db, "runs", "trigger_context", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// addColumn adds column to table if it is not already present. SQLite has no
// ADD COLUMN IF NOT EXISTS, so a duplicate-column error on re-run is treated as
// success, making migrate idempotent.
func addColumn(db *sql.DB, table, column, decl string) error {
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl))
	if err != nil && strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("orchestrator/store: add column %s.%s: %w", table, column, err)
	}
	return nil
}
