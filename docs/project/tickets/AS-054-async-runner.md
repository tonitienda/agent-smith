---
id: AS-054
title: Background/async runner (queue, scheduled, resumable, budget-capped)
status: ready-to-implement
github_issue: 54
depends_on: [AS-007, AS-041, AS-051]
area: async
priority: P2
source: PRD.md §7.22, §3 (Async Ana)
---

# AS-054 · Background/async runner

**Status: ready to implement**

## Description

§7.22: fire-and-forget and scheduled runs; queue; resumable; hard budget ceilings — the "cheap optimized engine for background tasks" use case and Async Ana's core need (§3: thousands of cheap, reliable, auditable runs unattended). The building blocks exist by now (headless runs AS-051, budgets AS-041, persistent sessions AS-007); what's unspecified is the process and operational model.

## Clarified implementation decisions

- **Process model:** V1 is local-first and does not require a daemon. Agent Smith owns durable queue/run bookkeeping; users run workers explicitly (`smith runs work`) or through their scheduler/CI. A daemon can be a later wrapper over the same queue.
- **Queue semantics:** queue lives under the existing Smith data directory, records immutable run/session IDs, defaults to one concurrent worker, and retries only provider/network transient failures within the configured budget.
- **Scheduling:** V1 supports one-shot queued/deferred runs. Recurring schedules are delegated to cron/CI/system schedulers.
- **Completion surface:** minimum viable surfaces are machine-readable run records, `smith runs list/status`, exit codes, and optional JSON output. Webhooks/desktop notifications are deferred.
- **Resumable:** interrupted runs are marked `interrupted`; manual `smith runs resume <id>` is required for V1. No automatic daemon restart semantics.

## Acceptance criteria

- [ ] `smith run --queue <task>` enqueues; the runner executes unattended within its budget ceiling and records a normal, auditable session (AS-007/AS-055).
- [ ] Hard budget stop on a background run halts cleanly and is reported in run status.
- [ ] A killed runner resumes or cleanly reports interrupted runs per the chosen policy.
- [ ] `smith runs list/status` shows queue and outcomes machine-readably.

## Dependencies

- AS-051 (headless execution), AS-041 (ceilings), AS-007 (run artifacts); AS-042 (cheap routing) soft
