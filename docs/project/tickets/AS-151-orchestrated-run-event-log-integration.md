---
id: AS-151
title: Smith event-log integration for orchestrated runs
status: ready-to-implement
area: observability
priority: P2
depends_on: [AS-161]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-151 · Smith event-log integration for orchestrated runs

## Description

Ensure every orchestrated job/run is persisted as a normal Smith append-only session with additional metadata for schedule, trigger, provider role, GitHub refs, PR links, artifacts, policy decisions, and cost.

## Acceptance criteria

- [ ] Orchestrated runs create readable/resumable Smith sessions rather than a separate log format.
- [ ] Session metadata links job ID, trigger ID, run DB ID, attempt number, provider role, GitHub refs, PR links, and artifact IDs.
- [ ] GitHub actions, policy checks, provider calls, costs, and terminal outcomes are represented as event-log blocks or referenced metadata.
- [ ] Cost and insights readers can process orchestrated sessions without a separate code path.
- [ ] Large artifacts are integrity-checked and referenced rather than embedded in the JSONL event log.

## Clarification (resolved 2026-06-30)

This ticket carried no ticket-local open question; it was held at
`needs-clarification` pending the [orchestrator ADR](../../architecture/orchestrator-architecture.md)
(AS-159), now **Accepted**, and AS-161 (daemon/scheduler/SQLite run store), now
**done**. The ADR's D-ORCH-4 boundary table assigns this exact seam: "Smith
session / event log | core (AS-005/006/007) | Each run is a normal append-only
Smith session; `/context`, `/cost`, `/insights`, replay reuse existing readers
(AS-151). No second observability path." AS-161's own status note confirms the
split it builds on: "narrative/cost stays in the session log" while the SQLite
run store holds only run-control state (jobs/triggers/runs/leases/attempts/
idempotency/audit). So the design is fixed — extend the existing append-only
session/event-log readers with the metadata this ticket's acceptance criteria
list, rather than introducing a parallel log format.

## Dependencies

[AS-161]
