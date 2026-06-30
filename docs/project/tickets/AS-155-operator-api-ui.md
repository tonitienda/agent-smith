---
id: AS-155
title: Operator API/UI
status: ready-to-implement
area: orchestrator
priority: P2
depends_on: [AS-161, AS-151]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-155 · Operator API/UI

## Description

Define the minimal operator surface for inspecting and controlling always-on Smith jobs before any hosted product UI exists.

## Acceptance criteria

- [ ] Minimal API/CLI operations cover list jobs, validate specs, dispatch, pause, resume, cancel, rerun, inspect status, and view run history.
- [ ] Operator output shows job ID, trigger, state, attempt, cost, provider role, GitHub links, policy gates, artifacts, and failure reason.
- [ ] Approval and merge actions are policy-checked and audited.
- [ ] The first dogfood surface can be CLI-first, with a later UI/API seam documented.
- [ ] The design avoids coupling the orchestrator to the existing local `smith serve` face until AS-159 decides that boundary.

## Clarification (resolved 2026-06-30)

The named blocker — "until AS-159 decides that boundary" — is resolved: AS-159
is now **Accepted**. The [orchestrator ADR](../../architecture/orchestrator-architecture.md)
fixes both halves of this ticket's open question:

- **Command surface (D-ORCH-2):** operator control lives under `smith runs
  daemon {list,inspect,rerun,cancel,pause,resume,health}` (CLI-first); "the
  operator API/UI in AS-155 wraps the same verbs, it does not replace them."
- **Relationship to `smith serve` (D-ORCH-4):** "Operator API/UI | AS-155 | Thin
  wrapper over the same run-control verbs; not a control path of its own." So
  this ticket is explicitly **not** coupled to the existing local `smith serve`
  face (AS-077) — it is a thin wrapper over the `internal/orchestrator`
  run-control verbs, which keeps it independent of whether/how `smith serve`
  evolves.

AS-161 (the daemon/run store these verbs operate on) and AS-151 (the event-log
integration the "view run history" criterion reads from) are both `done`/
`ready-to-implement`, so no remaining design question blocks this ticket.

## Dependencies

[AS-161, AS-151]
