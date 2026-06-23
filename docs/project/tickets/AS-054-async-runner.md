---
id: AS-054
title: Background/async runner (queue, scheduled, resumable, budget-capped)
status: done
github_issue: 54
depends_on: [AS-007, AS-041, AS-051]
area: async
priority: P2
source: PRD.md §7.22, §3 (Async Ana)
---

# AS-054 · Background/async runner

**Status: done**

## Description

§7.22: fire-and-forget and scheduled runs; queue; resumable; hard budget ceilings — the "cheap optimized engine for background tasks" use case and Async Ana's core need (§3: thousands of cheap, reliable, auditable runs unattended). The building blocks exist by now (headless runs AS-051, budgets AS-041, persistent sessions AS-007); what's unspecified is the process and operational model.

## Clarified implementation decisions

- **Process model:** V1 is local-first and does not require a daemon. Agent Smith owns durable queue/run bookkeeping; users run workers explicitly (`smith runs work`) or through their scheduler/CI. A daemon can be a later wrapper over the same queue.
- **Queue semantics:** queue lives under the existing Smith data directory, records immutable run/session IDs, defaults to one concurrent worker, and retries only provider/network transient failures within the configured budget.
- **Scheduling:** V1 supports one-shot queued/deferred runs. Recurring schedules are delegated to cron/CI/system schedulers.
- **Completion surface:** minimum viable surfaces are machine-readable run records, `smith runs list/status`, exit codes, and optional JSON output. Webhooks/desktop notifications are deferred.
- **Resumable:** interrupted runs are marked `interrupted`; manual `smith runs resume <id>` is required for V1. No automatic daemon restart semantics.

## Acceptance criteria

- [x] `smith run --queue <task>` enqueues; the runner executes unattended within its budget ceiling and records a normal, auditable session (AS-007/AS-055).
- [x] Hard budget stop on a background run halts cleanly and is reported in run status.
- [x] A killed runner resumes or cleanly reports interrupted runs per the chosen policy.
- [x] `smith runs list/status` shows queue and outcomes machine-readably.

## Implementation notes

- **Queue store (`internal/run`):** a project-scoped, stdlib-only durable store
  rooted at `~/.agent-smith/runs/<project-hash>/<run-id>/run.json` — the same
  data-dir layout as the session store, so a run's records sit beside the sessions
  it creates. Records carry the prompt, budget/auto posture, status, and the
  outcome (session id, stop reason, cost, exit code, error). Writes are atomic
  (temp file + rename + dir fsync); the package is execution-free and offline-
  testable (AS-095, PRD D6).
- **Enqueue:** `smith run --queue "<task>"` records a queued run and prints its ID
  instead of building an engine. `--budget`/`--auto` are captured on the record.
- **Worker:** `smith runs work` drains the queue FIFO, one run at a time (the V1
  one-concurrent-worker model), executing each through the shared headless core
  (`executeRun`, extracted from `runHeadless` so `smith run` and the worker
  classify outcomes identically). It maps the headless exit-code taxonomy
  (D-CLI-7) to a durable status: `done`/`budget`/`failed`/`interrupted`. Ctrl+C
  cancels the in-flight run cleanly (marked `interrupted`) and stops the worker.
- **Interrupted/resume:** on start the worker reclaims any record left `running`
  by a dead worker as `interrupted` (single-worker assumption); `smith runs
  resume <id>` re-queues an interrupted/failed/budget/canceled run. No automatic
  daemon restart (clarified decision).
- **Transient retries** are handled *inside* the turn by the loop's backoff policy
  (AS-018), so there is no redundant run-level retry: a run that still ends in a
  provider error has exhausted those retries, is recorded as failed, and is
  recoverable via `runs resume` — avoiding a double-spend and a forked session.
- **Inspection:** `smith runs list` (newest-first table / JSON array) and `smith
  runs status <id>` (key:value block / JSON object) are scriptable and emit
  machine-readable JSON under `--output json`.
- **Deferred (documented, not silent):** recurring schedules are delegated to
  cron/CI; a long-running worker daemon + concurrency > 1 is spun out as a
  follow-on (AS-132); webhooks/desktop notifications remain deferred per the
  clarified decisions above.

## Dependencies

- AS-051 (headless execution), AS-041 (ceilings), AS-007 (run artifacts); AS-042 (cheap routing) soft
