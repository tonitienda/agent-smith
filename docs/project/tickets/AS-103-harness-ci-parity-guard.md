---
id: AS-103
title: Guard CI and local harness parity
status: ready-to-implement
github_issue: 186
depends_on: [AS-100]
area: quality
priority: P2
source: docs/projects/harness-quality-system.md
---

# AS-103 · Guard CI and local harness parity

**Status: ready to implement**

## Description

Add a lightweight check that prevents CI from drifting away from the repository-owned local harness. When CI adds or removes a required quality job, the local command mapping and harness documentation should change in the same PR.

## Acceptance criteria

- [ ] Add a documented CI/local parity table to `docs/agent-quality-gates.md` or a linked harness doc.
- [ ] Add a test or script that verifies every required CI quality job has a documented local harness command.
- [ ] CI runs the parity guard as part of the quality workflow.
- [ ] The guard is deterministic and does not require network access.
- [ ] Documentation explains how to update the mapping when CI changes.

## Dependencies

- AS-100 (harness scripts)
