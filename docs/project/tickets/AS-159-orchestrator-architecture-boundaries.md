---
id: AS-159
title: Orchestrator architecture and product boundaries
status: done
area: orchestrator
priority: P2
depends_on: []
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-159 · Orchestrator architecture and product boundaries

## Description

Define the orchestrator-first architecture for Smith dogfood automation. This replaces the cloud-first framing with an always-on deterministic workflow engine that can run locally, on a private VPC host, and later as a hosted product.

## Acceptance criteria

- [ ] Architecture decision record states that Smith owns workflow state, schedules, GitHub actions, permissions, budgets, labels, and merge policy while models perform bounded cognitive work.
- [ ] Documented boundaries cover daemon, run DB, Smith session/event log, GitHub integration, provider routing, secrets, sandbox seam, and future operator UI/API.
- [ ] Deployment modes are separated: local daemon, private VPC daemon, remote workers/sandboxes, and future hosted execution.
- [ ] Non-goals explicitly include Smith editing its own job specs, jobs creating jobs, prompt-controlled labels/merges, and ignoring repository protection rules.
- [ ] Open questions from the PRD are triaged into follow-up decisions or scoped tickets.
- [ ] Downstream tickets are marked ready or kept needs-clarification based on the ADR output.

## Dependencies

[]

## Open questions

1. Should the first command shape be `smithd`, `smith runs daemon`, or another noun-grouped command?
2. Should the orchestrator live under existing packages first, or behind a new `internal/orchestrator` boundary?
3. Which decisions must be made before the remaining tickets in the wave (AS-147–AS-157, AS-160–AS-161) can move to ready-to-implement?
