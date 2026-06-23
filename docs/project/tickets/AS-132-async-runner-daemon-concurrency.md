---
id: AS-132
title: Background runner daemon + worker concurrency (spun out of AS-054)
status: done
github_issue: 401
depends_on: [AS-054]
area: async
priority: P2
source: PRD.md §7.22; AS-054 clarified decisions
---

# AS-132 · Background runner daemon + worker concurrency

**Status: ready to implement**

## Description

AS-054 shipped the durable run queue and a one-shot, single-worker
`smith runs work` that drains the queue and exits — the local-first, daemon-free
V1 model. Two pieces were deliberately deferred there and are tracked here so they
are not lost:

- **Long-running worker / watch mode.** A `smith runs work --watch` (or a thin
  daemon wrapper) that stays up and picks runs as they are enqueued, instead of
  draining once and exiting. The queue already records immutable run IDs and
  atomic status transitions, so this is a loop + signal-handling concern over the
  same store, not a storage change.
- **Worker concurrency > 1.** AS-054 assumes a single worker and reclaims any
  `running` record as `interrupted` on start. Concurrency needs a real claim
  mechanism (a lease/lock per record, or a worker-id + heartbeat) so two workers
  never run the same record and a crashed worker's runs are reclaimed without
  stealing a live worker's in-flight run.

## Acceptance criteria

- [ ] A watch-mode worker stays running and executes runs enqueued after it
      started, until interrupted; Ctrl+C drains/cancels cleanly.
- [ ] Two concurrent workers never execute the same run; a crashed worker's run is
      reclaimed as interrupted without disturbing a live worker.
- [ ] The single-worker `smith runs work` behaviour (drain-and-exit) still works
      unchanged.

## Resolved decisions

- **Claim mechanism (was an open question): both, each for what it is good at.** An
  `O_EXCL` lease file (`<run-id>/lease`) is the atomic claim *gate* — among any number
  of contending workers exactly one can create it, so the queued→running transition
  has a single winner without a filesystem advisory lock. *Liveness* is tracked
  separately by an additive `heartbeat_at` (plus `worker_id`) on the record (PRD D2): a
  worker refreshes it every 5s while executing, and `Reclaim` marks a `running` run
  `interrupted` only when its heartbeat is missing or older than the 30s staleness
  threshold — so a live peer's in-flight run is never stolen, while a crashed worker's
  run is recovered. The lease is released on completion and on `runs resume`, so a
  re-queued run can be re-claimed. A missing/nil heartbeat counts as stale, which keeps
  the AS-054 "reclaim a leftover `running` on start" behaviour intact for the single
  worker.
- **Watch + concurrency surface.** `smith runs work --watch` keeps the worker up,
  polling for new work; `--concurrency N` (default 1) runs N claim/execute loops in
  parallel. Default `smith runs work` (drain-and-exit, single worker) is unchanged.

## Dependencies

- AS-054 (the queue store + single-worker drain this extends)
