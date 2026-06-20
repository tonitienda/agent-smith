---
id: AS-101
title: Agent and local hook integration for the harness
status: ready-to-implement
github_issue: null
depends_on: [AS-100]
area: quality
priority: P1
source: docs/projects/harness-quality-system.md
---

# AS-101 · Agent and local hook integration for the harness

**Status: ready to implement**

## Description

Wire the harness into practical hook surfaces without making the project depend on any single agent provider. Claude should get sample hook configuration, Codex should get explicit pre-commit/pre-handoff instructions plus Git hook support, and humans should be able to install the same local hooks.

## Acceptance criteria

- [ ] Add a documented local Git hook installer for pre-commit and optional pre-push harness checks.
- [ ] Add sample Claude hook configuration that delegates to `scripts/harness/quick.sh` and `scripts/harness/full.sh`.
- [ ] Document the Codex equivalent workflow: instructions + local Git hooks + mandatory final full gate before commit.
- [ ] Document how future Smith lifecycle hooks should call the same scripts instead of duplicating logic.
- [ ] Hook failures surface command output clearly and do not hide failures from the agent transcript.

## Dependencies

- AS-100 (harness scripts)
