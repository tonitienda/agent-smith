---
id: AS-155
title: Operator API/UI
status: needs-clarification
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

## Dependencies

[AS-161, AS-151]
