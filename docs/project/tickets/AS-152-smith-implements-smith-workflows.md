---
id: AS-152
title: Smith implements Smith dogfood workflow pack
status: ready-to-implement
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

## Clarification (resolved 2026-06-30)

This ticket carried no ticket-local open question; the example job spec already
in the [dogfood PRD §4.1](../smith-orchestrator-dogfood-prd.md) (the
`implement-labeled-work` YAML with triggers, concurrency, steps, provider
routing, permissions, and `merge_policy`) is a concrete worked example of
exactly what this ticket's acceptance criteria ask for, and every dependency
(AS-147, AS-149, AS-150, AS-151, AS-157) now has its design fixed (all moved to
`ready-to-implement` in this pass), with AS-160/AS-161 already `done`. Per the
[ticket-implementer](../../../.claude/skills/ticket-implementer/SKILL.md)
convention, `ready-to-implement` records that no open design question remains —
build sequencing (this is naturally one of the last tickets in the wave, since
it composes all the others) is tracked via `depends_on` and the README's
suggested build order, not via status.

## Dependencies

[AS-160, AS-161, AS-147, AS-149, AS-150, AS-151, AS-157]
