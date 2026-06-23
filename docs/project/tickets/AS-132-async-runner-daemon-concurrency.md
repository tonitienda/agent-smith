---
id: AS-132
title: Background runner daemon + worker concurrency (spun out of AS-054)
status: ready-to-implement
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

## Open questions

- Claim mechanism: per-record lock file vs. worker-id + heartbeat timestamp in the
  record. The heartbeat approach reclaims stale leases without filesystem locks and
  fits the additive record shape, but needs a staleness threshold.

## Dependencies

- AS-054 (the queue store + single-worker drain this extends)
