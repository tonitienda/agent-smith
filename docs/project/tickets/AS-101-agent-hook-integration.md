---
id: AS-101
title: Agent and local hook integration for the harness
status: done
github_issue: 184
depends_on: [AS-100]
area: quality
priority: P1
source: docs/projects/harness-quality-system.md
---

# AS-101 · Agent and local hook integration for the harness

**Status: done**

Implemented: repo-owned Git hooks in `.githooks/` (`pre-commit` → quick gate,
opt-in `pre-push` → full gate) with `scripts/harness/install-git-hooks.sh`
installer (sets `core.hooksPath`, `--with-pre-push`, `--uninstall`); sample
Claude hook config in `docs/examples/claude-harness-hooks.json`
(PostToolUse → quick, Stop → full); and a Hook integration section in
`docs/agent-quality-gates.md` covering local Git hooks, Claude, the Codex
workflow, and future Smith lifecycle hooks. All surfaces delegate to the same
`scripts/harness/*.sh` commands, which print each command and preserve exit
codes so failures stay visible.

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
