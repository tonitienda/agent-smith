---
id: AS-153
title: Cloud operator UI/API for schedules, runs, costs, and approvals
status: ready-to-implement
area: cloud
priority: P2
depends_on: [AS-146, AS-151]
source: docs/projects/smith-cloud-prd.md
---

# AS-153 · Cloud operator UI/API for schedules, runs, costs, and approvals

## Description

Expose cloud schedules and runs through APIs and an operator UI suitable for dogfood operations.

## Acceptance criteria

- [ ] Operators can list/create/update/disable schedules, manually dispatch runs, cancel in-flight runs, and inspect run status.
- [ ] UI/API surfaces cost, model/provider, sandbox telemetry, PR links, artifacts, logs, approval gates, and failure reasons.
- [ ] Approval actions are policy-checked and audited.
- [ ] CLI commands can validate a job spec and dispatch/cancel a cloud run against the same API.

## Dependencies

[AS-146, AS-151]
