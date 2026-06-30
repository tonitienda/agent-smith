---
id: AS-152
title: Smith implements Smith dogfood workflow pack
status: needs-clarification
area: dogfood
priority: P2
depends_on: [AS-160, AS-161, AS-147, AS-149, AS-150, AS-151, AS-157]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-152 · Smith implements Smith dogfood workflow pack

## Description

Define the first private job specs that let Smith implement and maintain Agent Smith itself from GitHub labels, schedules, and merge events.

## Acceptance criteria

- [ ] Adds reviewed example job specs for implementation-labeled issues/PRs, documentation drift, backlog hygiene, post-merge follow-up, architecture checks, and manual-test simulation.
- [ ] Each job declares triggers, budgets, concurrency, provider roles, GitHub access, deterministic label/PR hooks, and merge policy.
- [ ] Initial workflows use deterministic labels such as `implementation`, `smith-generated`, and `smith-auto-merge` without requiring the model prompt to remember them.
- [ ] Runbooks explain how to pause jobs, inspect failures, rerun a job, and disable auto-merge policy.
- [ ] The first pack is scoped to `tonitienda/agent-smith` and private maintainer dogfood.

## Dependencies

[AS-160, AS-161, AS-147, AS-149, AS-150, AS-151, AS-157]

## Open questions

1. First monthly/per-run budget ceilings (PRD Q9) and the concrete dogfood job set depend on AS-160/AS-161 and the policy tickets (AS-157) landing.
