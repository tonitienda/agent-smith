---
id: AS-161
title: Daemon, scheduler, and SQLite run store
status: ready-to-implement
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

- [ ] Command shape and package boundary are decided and documented.
- [ ] SQLite schema covers jobs, triggers, queued runs, active leases, attempts, terminal state, idempotency keys, and audit entries.
- [ ] Scheduler supports cron/manual/GitHub-triggered enqueue paths using the same run model.
- [ ] Run execution is bounded by concurrency, timeout, retry, and budget policy.
- [ ] Operators can pause, resume, rerun, cancel, inspect, and health-check the daemon from CLI/API surfaces.
- [ ] Failure states clearly distinguish missing permissions, missing secrets, invalid job specs, budget exhaustion, and blocked policy.

## Dependencies

[AS-159, AS-160]
