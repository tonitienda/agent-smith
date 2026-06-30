package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by lookups when no row matches.
var ErrNotFound = errors.New("orchestrator/store: not found")

// UpsertJob inserts or replaces a job's control row and its trigger rows. It is
// how the loader publishes the specs it parsed from `.agent-smith/jobs/`; an
// existing job's paused flag is preserved across a reload.
func (s *Store) UpsertJob(j Job, triggers []JobTrigger) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var paused bool
	_ = tx.QueryRow(`SELECT paused FROM jobs WHERE id = ?`, j.ID).Scan(&paused)

	if _, err := tx.Exec(`
		INSERT INTO jobs (id, file, version, owner, repository, org, spec_json, paused, loaded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			file=excluded.file, version=excluded.version, owner=excluded.owner,
			repository=excluded.repository, org=excluded.org, spec_json=excluded.spec_json,
			loaded_at=excluded.loaded_at`,
		j.ID, j.File, j.Version, j.Owner, j.Repository, j.Org, j.SpecJSON, boolToInt(paused), j.LoadedAt.UTC(),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM triggers WHERE job_id = ?`, j.ID); err != nil {
		return err
	}
	for _, t := range triggers {
		var next any
		if t.NextFire != nil {
			next = t.NextFire.UTC()
		}
		if _, err := tx.Exec(`INSERT INTO triggers (job_id, kind, args_json, next_fire) VALUES (?, ?, ?, ?)`,
			j.ID, t.Kind, t.ArgsJSON, next); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// JobTrigger is a trigger row attached to a job at load time.
type JobTrigger struct {
	Kind     string
	ArgsJSON string
	NextFire *time.Time
}

// SetJobPaused toggles a job's paused flag; a paused job's triggers do not enqueue
// new runs (in-flight runs are unaffected).
func (s *Store) SetJobPaused(id string, paused bool) error {
	res, err := s.db.Exec(`UPDATE jobs SET paused = ? WHERE id = ?`, boolToInt(paused), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Jobs returns every loaded job, ordered by id.
func (s *Store) Jobs() ([]Job, error) {
	rows, err := s.db.Query(`SELECT id, file, version, owner, repository, org, spec_json, paused, loaded_at FROM jobs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Job
	for rows.Next() {
		var j Job
		var paused int
		if err := rows.Scan(&j.ID, &j.File, &j.Version, &j.Owner, &j.Repository, &j.Org, &j.SpecJSON, &paused, &j.LoadedAt); err != nil {
			return nil, err
		}
		j.Paused = paused != 0
		out = append(out, j)
	}
	return out, rows.Err()
}

// Job looks up one job by id.
func (s *Store) Job(id string) (Job, error) {
	var j Job
	var paused int
	err := s.db.QueryRow(`SELECT id, file, version, owner, repository, org, spec_json, paused, loaded_at FROM jobs WHERE id = ?`, id).
		Scan(&j.ID, &j.File, &j.Version, &j.Owner, &j.Repository, &j.Org, &j.SpecJSON, &paused, &j.LoadedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	j.Paused = paused != 0
	return j, nil
}

// Enqueue records a new queued run. When NewRun.IdempotencyKey is non-empty and a
// run already exists under that key, the existing run is returned and nothing new
// is queued — this is how the scheduler dedupes a re-delivered trigger.
func (s *Store) Enqueue(n NewRun, now time.Time) (Run, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if n.IdempotencyKey != "" {
		var existing string
		err := tx.QueryRow(`SELECT run_id FROM idempotency_keys WHERE key = ?`, n.IdempotencyKey).Scan(&existing)
		if err == nil {
			run, err := s.runByIDTx(tx, existing)
			if err != nil {
				return Run{}, err
			}
			return run, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Run{}, err
		}
	}

	limit := n.ConcurrencyLimit
	if limit < 1 {
		limit = 1
	}
	maxAttempts := n.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	id := newRunID()
	if _, err := tx.Exec(`
		INSERT INTO runs (id, job_id, trigger_kind, concurrency_key, concurrency_limit,
			idempotency_key, status, attempt, max_attempts, budget_usd, timeout_secs, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		id, n.JobID, n.TriggerKind, n.ConcurrencyKey, limit, n.IdempotencyKey,
		string(StatusQueued), maxAttempts, n.BudgetUSD, int(n.Timeout.Seconds()), now.UTC(),
	); err != nil {
		return Run{}, err
	}
	if n.IdempotencyKey != "" {
		if _, err := tx.Exec(`INSERT INTO idempotency_keys (key, run_id, created_at) VALUES (?, ?, ?)`,
			n.IdempotencyKey, id, now.UTC()); err != nil {
			return Run{}, err
		}
	}
	if err := auditTx(tx, now, n.JobID, id, "enqueue", n.TriggerKind); err != nil {
		return Run{}, err
	}
	run, err := s.runByIDTx(tx, id)
	if err != nil {
		return Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return Run{}, err
	}
	return run, nil
}

// ClaimNext atomically claims the oldest queued run whose concurrency key has room
// (fewer running than its limit), marks it running under workerID, and records the
// attempt. ok is false when nothing is claimable. This is the scheduler's bounded
// dispatch primitive — the limit gate plus the single-connection write guarantee
// no two workers exceed a key's concurrency.
func (s *Store) ClaimNext(workerID string, now time.Time) (Run, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Run{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT id, concurrency_key, concurrency_limit, attempt FROM runs WHERE status = ? ORDER BY queued_at, id`, string(StatusQueued))
	if err != nil {
		return Run{}, false, err
	}
	type cand struct {
		id    string
		key   string
		limit int
		att   int
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.key, &c.limit, &c.att); err != nil {
			_ = rows.Close()
			return Run{}, false, err
		}
		cands = append(cands, c)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return Run{}, false, err
	}

	for _, c := range cands {
		var running int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM runs WHERE concurrency_key = ? AND status = ?`,
			c.key, string(StatusRunning)).Scan(&running); err != nil {
			return Run{}, false, err
		}
		if c.key != "" && running >= c.limit {
			continue // key saturated; try the next queued run
		}
		attempt := c.att + 1
		if _, err := tx.Exec(`UPDATE runs SET status = ?, worker_id = ?, attempt = ?, started_at = ?, heartbeat_at = ? WHERE id = ?`,
			string(StatusRunning), workerID, attempt, now.UTC(), now.UTC(), c.id); err != nil {
			return Run{}, false, err
		}
		if _, err := tx.Exec(`INSERT INTO attempts (run_id, number, status, started_at) VALUES (?, ?, ?, ?)`,
			c.id, attempt, string(StatusRunning), now.UTC()); err != nil {
			return Run{}, false, err
		}
		run, err := s.runByIDTx(tx, c.id)
		if err != nil {
			return Run{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Run{}, false, err
		}
		return run, true, nil
	}
	return Run{}, false, nil
}

// Heartbeat refreshes a running run's liveness timestamp so ReclaimStale does not
// requeue a run that is still progressing. A heartbeat from a worker that no longer
// holds the lease is a no-op.
func (s *Store) Heartbeat(runID, workerID string, now time.Time) error {
	_, err := s.db.Exec(`UPDATE runs SET heartbeat_at = ? WHERE id = ? AND worker_id = ? AND status = ?`,
		now.UTC(), runID, workerID, string(StatusRunning))
	return err
}

// Outcome is the terminal result a worker writes back for a run.
type Outcome struct {
	Status       RunStatus
	FailureClass FailureClass
	SessionID    string
	CostUSD      float64
	Error        string
}

// Finish records a run's terminal outcome and closes its open attempt. A
// non-terminal status is rejected — Finish ends a run.
func (s *Store) Finish(runID string, out Outcome, now time.Time) error {
	if !out.Status.Terminal() {
		return fmt.Errorf("orchestrator/store: Finish needs a terminal status, got %q", out.Status)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`UPDATE runs SET status = ?, failure_class = ?, session_id = ?, cost_usd = ?, error = ?, finished_at = ?, worker_id = '' WHERE id = ?`,
		string(out.Status), string(out.FailureClass), out.SessionID, out.CostUSD, out.Error, now.UTC(), runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE attempts SET status = ?, failure_class = ?, error = ?, finished_at = ? WHERE run_id = ? AND finished_at IS NULL`,
		string(out.Status), string(out.FailureClass), out.Error, now.UTC(), runID); err != nil {
		return err
	}
	if err := auditTx(tx, now, "", runID, "finish", string(out.Status)); err != nil {
		return err
	}
	return tx.Commit()
}

// Requeue returns a running run to the queue so a worker can re-claim it (a retry
// or a stale-lease recovery). It refuses a run that has exhausted its attempts,
// returning ok=false so the caller fails it instead.
func (s *Store) Requeue(runID string, now time.Time) (ok bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var attempt, maxAttempts int
	var status string
	err = tx.QueryRow(`SELECT attempt, max_attempts, status FROM runs WHERE id = ?`, runID).Scan(&attempt, &maxAttempts, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if attempt >= maxAttempts {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE runs SET status = ?, worker_id = '', heartbeat_at = NULL WHERE id = ?`,
		string(StatusQueued), runID); err != nil {
		return false, err
	}
	if err := auditTx(tx, now, "", runID, "requeue", ""); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ReclaimStale requeues (or fails, when out of attempts) running runs whose
// heartbeat is older than staleAfter — the recovery path for a worker that died
// mid-run. It returns the run ids it acted on.
func (s *Store) ReclaimStale(staleAfter time.Duration, now time.Time) ([]string, error) {
	cutoff := now.UTC().Add(-staleAfter)
	rows, err := s.db.Query(`SELECT id FROM runs WHERE status = ? AND (heartbeat_at IS NULL OR heartbeat_at < ?)`,
		string(StatusRunning), cutoff)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var acted []string
	for _, id := range ids {
		ok, err := s.Requeue(id, now)
		if err != nil {
			return acted, err
		}
		if !ok {
			if err := s.Finish(id, Outcome{Status: StatusFailed, FailureClass: FailureInternal, Error: "worker lost; retries exhausted"}, now); err != nil {
				return acted, err
			}
		}
		acted = append(acted, id)
	}
	return acted, nil
}

// Cancel marks a queued or running run canceled (operator action). A terminal run
// is left alone, returning ok=false.
func (s *Store) Cancel(runID string, now time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	err = tx.QueryRow(`SELECT status FROM runs WHERE id = ?`, runID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if RunStatus(status).Terminal() {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE runs SET status = ?, worker_id = '', finished_at = ? WHERE id = ?`,
		string(StatusCanceled), now.UTC(), runID); err != nil {
		return false, err
	}
	if err := auditTx(tx, now, "", runID, "cancel", ""); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Rerun enqueues a fresh run cloning a terminal run's job and policy (a new
// identity and a new idempotency key, so it is a genuinely new unit of work).
func (s *Store) Rerun(runID string, now time.Time) (Run, error) {
	src, err := s.Run(runID)
	if err != nil {
		return Run{}, err
	}
	if !src.Status.Terminal() {
		return Run{}, fmt.Errorf("orchestrator/store: run %s is %s, not rerunnable", runID, src.Status)
	}
	return s.Enqueue(NewRun{
		JobID:            src.JobID,
		TriggerKind:      src.TriggerKind,
		ConcurrencyKey:   src.ConcurrencyKey,
		ConcurrencyLimit: src.ConcurrencyLimit,
		IdempotencyKey:   "rerun:" + runID + ":" + newRunID(),
		BudgetUSD:        src.BudgetUSD,
		Timeout:          time.Duration(src.TimeoutSecs) * time.Second,
		MaxAttempts:      src.MaxAttempts,
	}, now)
}

// FailAttempt closes a run's open attempt as failed without finishing the run —
// the step before Requeue on a retry, so the attempt history records the failed
// try while the run returns to the queue.
func (s *Store) FailAttempt(runID string, fc FailureClass, errMsg string, now time.Time) error {
	_, err := s.db.Exec(`UPDATE attempts SET status = ?, failure_class = ?, error = ?, finished_at = ? WHERE run_id = ? AND finished_at IS NULL`,
		string(StatusFailed), string(fc), errMsg, now.UTC(), runID)
	return err
}

// RunByIdempotencyKey returns the run previously enqueued under key, if any. The
// scheduler consults it before applying concurrency on_conflict so a re-delivered
// trigger maps back to its existing run rather than being treated as a conflict.
func (s *Store) RunByIdempotencyKey(key string) (Run, bool, error) {
	if key == "" {
		return Run{}, false, nil
	}
	var runID string
	err := s.db.QueryRow(`SELECT run_id FROM idempotency_keys WHERE key = ?`, key).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	r, err := s.Run(runID)
	if err != nil {
		return Run{}, false, err
	}
	return r, true, nil
}

// ActiveRunsByKey returns the queued and running runs sharing a concurrency key —
// the scheduler reads it to apply a trigger's on_conflict policy (queue / drop /
// cancel-running) at enqueue time.
func (s *Store) ActiveRunsByKey(key string) ([]Run, error) {
	rows, err := s.db.Query(`SELECT `+runCols+` FROM runs WHERE concurrency_key = ? AND status IN (?, ?) ORDER BY queued_at`,
		key, string(StatusQueued), string(StatusRunning))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Run looks up one run by id.
func (s *Store) Run(id string) (Run, error) {
	return s.runByIDTx(s.db, id)
}

// Runs returns runs, newest first. A non-empty status filters to that status.
func (s *Store) Runs(status RunStatus) ([]Run, error) {
	q := `SELECT ` + runCols + ` FROM runs`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY queued_at DESC, id DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Attempts returns a run's attempt history, oldest first.
func (s *Store) Attempts(runID string) ([]Attempt, error) {
	rows, err := s.db.Query(`SELECT run_id, number, status, failure_class, error, started_at, finished_at FROM attempts WHERE run_id = ? ORDER BY number`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Attempt
	for rows.Next() {
		var a Attempt
		var fc, st string
		if err := rows.Scan(&a.RunID, &a.Number, &st, &fc, &a.Error, &a.StartedAt, &a.FinishedAt); err != nil {
			return nil, err
		}
		a.Status, a.FailureClass = RunStatus(st), FailureClass(fc)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Audit records an operator/lifecycle action outside a run transaction.
func (s *Store) Audit(e AuditEntry, now time.Time) error {
	return auditTx(s.db, now, e.JobID, e.RunID, e.Action, e.Detail)
}

// AuditEntries returns the most recent audit rows, newest first, up to limit.
func (s *Store) AuditEntries(limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT at, job_id, run_id, action, detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.At, &e.JobID, &e.RunID, &e.Action, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- internal helpers ---

const runCols = `id, job_id, trigger_kind, concurrency_key, concurrency_limit, idempotency_key,
	status, failure_class, attempt, max_attempts, budget_usd, timeout_secs, session_id, cost_usd,
	error, worker_id, queued_at, started_at, finished_at, heartbeat_at`

// rowScanner is the shared shape of *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(...any) error }

// querier is the shared shape of *sql.DB and *sql.Tx for read helpers.
type querier interface {
	QueryRow(string, ...any) *sql.Row
}

func (s *Store) runByIDTx(q querier, id string) (Run, error) {
	row := q.QueryRow(`SELECT `+runCols+` FROM runs WHERE id = ?`, id)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	return r, err
}

func scanRun(sc rowScanner) (Run, error) {
	var r Run
	var status, fc string
	if err := sc.Scan(&r.ID, &r.JobID, &r.TriggerKind, &r.ConcurrencyKey, &r.ConcurrencyLimit, &r.IdempotencyKey,
		&status, &fc, &r.Attempt, &r.MaxAttempts, &r.BudgetUSD, &r.TimeoutSecs, &r.SessionID, &r.CostUSD,
		&r.Error, &r.WorkerID, &r.QueuedAt, &r.StartedAt, &r.FinishedAt, &r.HeartbeatAt); err != nil {
		return Run{}, err
	}
	r.Status, r.FailureClass = RunStatus(status), FailureClass(fc)
	return r, nil
}

// execer is the shared shape of *sql.DB and *sql.Tx for the audit helper.
type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

func auditTx(e execer, now time.Time, jobID, runID, action, detail string) error {
	_, err := e.Exec(`INSERT INTO audit (at, job_id, run_id, action, detail) VALUES (?, ?, ?, ?, ?)`,
		now.UTC(), jobID, runID, action, detail)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func newRunID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "run_" + hex.EncodeToString(b[:])
}
