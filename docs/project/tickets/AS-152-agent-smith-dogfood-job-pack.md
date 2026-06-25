---
id: AS-152
title: Dogfood job pack for the Agent Smith repository
status: ready-to-implement
area: dogfood
priority: P2
depends_on: [AS-145, AS-149, AS-150, AS-151]
source: docs/projects/smith-cloud-prd.md
---

# AS-152 · Dogfood job pack for the Agent Smith repository

## Description

Define the first private Smith Cloud jobs that replace the maintainer Assembly Line for this repository.

## Acceptance criteria

- [ ] Adds reviewed job specs for documentation drift, backlog/ticket hygiene, post-merge follow-up, and periodic quality sweeps.
- [ ] Each job declares budget, schedule/event trigger, sandbox profile, required secrets, GitHub permissions, and PR policy.
- [ ] Auto-merge remains off for the initial dogfood pack unless AS-150 policy has been explicitly approved.
- [ ] Runbooks explain how to pause schedules, inspect failures, and rerun jobs.

## Dependencies

[AS-145, AS-149, AS-150, AS-151]
