---
id: AS-161
title: Daemon, scheduler, and SQLite run store
status: done
area: orchestrator
priority: P2
depends_on: [AS-159, AS-160, AS-163]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-161 · Daemon, scheduler, and SQLite run store

## Description

Design and implement the first always-on Smith orchestrator process for loading job specs, receiving triggers, queuing work, tracking run state, and running dogfood jobs without requiring the maintainer laptop to stay open.

> **Scope note:** the job-spec *model + validator* (the §5 load-time validity
> contract) is carved out to **AS-163** (`internal/orchestrator/spec`, done). This
> ticket builds on it: the YAML/file loader over `.agent-smith/jobs/`, cross-file
> `id` collision (`spec.CheckUnique`), the SQLite run store, the scheduler, and the
> `smith runs daemon` + operator surfaces.

## Acceptance criteria

- [x] Command shape and package boundary are decided and documented.
- [x] SQLite schema covers jobs, triggers, queued runs, active leases, attempts, terminal state, idempotency keys, and audit entries.
- [x] Scheduler supports cron/manual/GitHub-triggered enqueue paths using the same run model.
- [x] Run execution is bounded by concurrency, timeout, retry, and budget policy.
- [x] Operators can pause, resume, rerun, cancel, inspect, and health-check the daemon from CLI/API surfaces.
- [x] Failure states clearly distinguish missing permissions, missing secrets, invalid job specs, budget exhaustion, and blocked policy.

## Implementation notes (MVP 0)

- **Packages.** `internal/orchestrator` (daemon, scheduler, cron parser, loader,
  `Executor` seam) at the orchestration tier; `internal/orchestrator/store` (the
  SQLite run-control store, pure-Go `modernc.org/sqlite` so `make build` stays a
  static, cgo-free binary). Both are archtest-guarded: third-party allowance for
  the subtree (boundaries), an orchestration-tier entry (inward-core guard), and
  per-package layering rules (daemon ↛ faces/cmd; store ↛ daemon-spec/loop/faces).
- **Command shape (ADR Q1 refinement).** `runs` already hosts the AS-054 async
  prompt-run queue (`list/status/work/resume`), a different notion of "run". To
  avoid overloading those verbs, the orchestrator nests under **`runs daemon`**:
  `smith runs daemon start` is the long-lived process, and
  `list/inspect/rerun/cancel/pause/resume/health` are the operator verbs. This
  keeps the ADR's `smith runs daemon` entry while disambiguating the surface
  (the router models groups and leaves separately, so the process is the explicit
  `start` leaf rather than a bare group).
- **Run store split (ADR Q3).** Schema covers `jobs`, `triggers`, `runs`,
  `idempotency_keys`, `attempts`, `audit`; per-run leasing (`worker_id` +
  `heartbeat_at`) is the active-lease state. Narrative + cost stay in the Smith
  session log — the store keeps only `session_id`, headline `cost_usd`, status,
  and `failure_class`.
- **Scheduler.** Cron (5-field + required IANA timezone), manual, and a normalised
  `GitHubEvent` enqueue path share one run model; concurrency `on_conflict`
  (queue/drop/cancel-running) and idempotency keys dedupe trigger fan-out
  (idempotency wins over on_conflict so a re-delivered event maps to its run).
- **Bounded execution.** Per-run timeout (context deadline), retry (attempts =
  `retries.max + 1`, retried only for transient internal/timeout failures —
  deterministic/fail-closed classes never retry), per-key concurrency limit, and
  budget ceiling carried on the run. Crashed-worker runs are reclaimed via the
  stale-heartbeat path.
- **Failure classes.** `store.FailureClass` distinguishes missing_permission,
  missing_secret, invalid_spec, budget_exhausted, blocked_policy, timeout, and
  internal — the contract operators and failure hooks read.
- **Executor seam.** The MVP-0 `StubExecutor` performs no model/GitHub work; the
  real executor (agent steps over a provider, deterministic GitHub actions, the
  session-log write) is wired by AS-150/AS-147/AS-149/AS-151 against this seam.

## Dependencies

[AS-159, AS-160]
