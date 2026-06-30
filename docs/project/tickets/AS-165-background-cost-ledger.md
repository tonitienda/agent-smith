---
id: AS-165
title: Background cost ledger and autonomous activity attribution
status: ready-to-implement
area: cost
priority: P1
depends_on: [AS-020, AS-041, AS-054, AS-120, AS-132]
source: docs/project/competitors.md
---

# AS-165 · Background cost ledger and autonomous activity attribution

## Description

Extend Smith's cost accounting so every background job, subagent, hook-triggered model call, model-assisted insight pass, and automatic review has clear attribution, budget ownership, and user-visible limits.

## Acceptance criteria

- [ ] Every autonomous activity is attributed to a parent session/job, trigger, provider/model, and budget bucket.
- [ ] `/cost` and daemon/operator views distinguish foreground user turns from background/autonomous spend.
- [ ] Users can set hard and soft limits for background activity separately from interactive turns.
- [ ] Budget exhaustion produces deterministic stop/degrade behavior with an event-log record.
- [ ] Reports include enough detail to explain unexpected spend without exposing secrets.

## Dependencies

[AS-020, AS-041, AS-054, AS-120, AS-132]
