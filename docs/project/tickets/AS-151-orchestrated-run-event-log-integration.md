---
id: AS-151
title: Smith event-log integration for orchestrated runs
status: needs-clarification
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

## Dependencies

[AS-161]
