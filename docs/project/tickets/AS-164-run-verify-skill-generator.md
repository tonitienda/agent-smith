---
id: AS-164
title: Run/verify skill generator
status: ready-to-implement
area: skills
priority: P1
depends_on: [AS-034, AS-035, AS-058, AS-099]
source: docs/project/competitors.md
---

# AS-164 · Run/verify skill generator

## Description

Teach Smith how to learn and persist project-specific launch and verification recipes, then expose them as portable skills/commands. This closes the gap between generic quality gates and real "prove the app works" workflows that require servers, env files, databases, browsers, or TUI driving.

## Acceptance criteria

- [ ] Smith can infer a first draft run/verify recipe from README, Makefile, package files, and prior successful commands.
- [ ] The user can approve, edit, and save the recipe into the appropriate project skill or memory location.
- [ ] The recipe records prerequisites, environment variables to set or redact, commands, readiness checks, and cleanup steps.
- [ ] A verification run emits structured artifacts and links them to the session event log.
- [ ] Failed inference is reported as an actionable missing-context item, not as a silent fallback.

## Dependencies

[AS-034, AS-035, AS-058, AS-099]
