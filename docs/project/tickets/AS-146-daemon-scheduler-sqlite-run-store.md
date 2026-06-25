---
id: AS-146
title: Daemon, scheduler, and SQLite run store
status: needs-clarification
area: orchestrator
priority: P2
depends_on: [AS-144, AS-145]
source: docs/projects/smith-orchestrator-dogfood-prd.md
---

# AS-146 · Daemon, scheduler, and SQLite run store

## Description

Design and implement the first always-on Smith orchestrator process for loading job specs, receiving triggers, queuing work, tracking run state, and running dogfood jobs without requiring the maintainer laptop to stay open.

## Acceptance criteria

- [ ] Command shape and package boundary are decided and documented.
- [ ] SQLite schema covers jobs, triggers, queued runs, active leases, attempts, terminal state, idempotency keys, and audit entries.
- [ ] Scheduler supports cron/manual/GitHub-triggered enqueue paths using the same run model.
- [ ] Run execution is bounded by concurrency, timeout, retry, and budget policy.
- [ ] Operators can pause, resume, rerun, cancel, inspect, and health-check the daemon from CLI/API surfaces.
- [ ] Failure states clearly distinguish missing permissions, missing secrets, invalid job specs, budget exhaustion, and blocked policy.

## Dependencies

[AS-144, AS-145]
