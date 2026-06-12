---
id: AS-054
title: Background/async runner (queue, scheduled, resumable, budget-capped)
status: needs-clarification
github_issue: null
depends_on: [AS-007, AS-041, AS-051]
area: async
priority: P2
source: PRD.md §7.22, §3 (Async Ana)
---

# AS-054 · Background/async runner

**Status: needs clarification**

## Description

§7.22: fire-and-forget and scheduled runs; queue; resumable; hard budget ceilings — the "cheap optimized engine for background tasks" use case and Async Ana's core need (§3: thousands of cheap, reliable, auditable runs unattended). The building blocks exist by now (headless runs AS-051, budgets AS-041, persistent sessions AS-007); what's unspecified is the process and operational model.

## Open questions (why this needs clarification)

1. **Process model** — a long-lived local daemon, on-demand spawned workers, or "bring your own scheduler" (cron/CI invokes `smith -p`) with Agent Smith providing only queue/run bookkeeping? (D8 says no cloud infra now — this is local-first.)
2. **Queue semantics** — persistence location, concurrency limits, retry policy on transient provider errors, dedupe of identical queued tasks?
3. **Scheduling** — does Agent Smith own recurring schedules (cron-like config) in v1, or only one-shot deferred runs?
4. **Completion surface** — how does Ana learn a run finished: file/exit artifact, webhook, desktop notification, `smith runs list`? Minimum viable set?
5. **Resumable** — auto-resume interrupted runs on daemon restart, or manual `smith runs resume <id>`?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] `smith run --queue <task>` enqueues; the runner executes unattended within its budget ceiling and records a normal, auditable session (AS-007/AS-055).
- [ ] Hard budget stop on a background run halts cleanly and is reported in run status.
- [ ] A killed runner resumes or cleanly reports interrupted runs per the chosen policy.
- [ ] `smith runs list/status` shows queue and outcomes machine-readably.

## Dependencies

- AS-051 (headless execution), AS-041 (ceilings), AS-007 (run artifacts); AS-042 (cheap routing) soft
