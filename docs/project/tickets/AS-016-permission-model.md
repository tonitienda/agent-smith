---
id: AS-016
title: Permission model (ask / allowlist / auto) + documented security posture
status: ready-to-implement
github_issue: 16
depends_on: [AS-013]
area: security
priority: P0
source: PRD.md D9, §7.2
---

# AS-016 · Permission model and security posture

**Status: ready to implement**

## Description

D9's "build now" item: the permission/approval model. Three modes — **ask** (prompt per action), **allowlist** (pre-approved patterns auto-run, rest ask), **auto** (everything runs) — applied per tool.

- Rule format in config: tool name + optional pattern (e.g., shell command prefix `git status*`, path globs for file writes). Project-level + user-level config, merged.
- Runtime decision API consumed by the tool runtime hook (AS-013): `allow | deny | ask`, with the ask path delegated to the active face (TUI prompt — AS-024).
- "Always allow this" at prompt time appends to the project allowlist.
- Denials return structured feedback to the model (it should adjust, not retry blindly).
- Deliverable alongside code: `docs/SECURITY.md` stating the D9 posture and the **explicitly documented known limits** (no OS sandbox, no prompt-injection defense in V1) — D0 requires punts to be documented, never silent.

## Acceptance criteria

- [ ] Each mode behaves per spec; mode is switchable per session and per tool.
- [ ] Allowlist patterns match correctly for shell prefixes and file-path globs (tested).
- [ ] No tool execution path bypasses the check (enforced by test on the runtime hook).
- [ ] `docs/SECURITY.md` ships with the stated posture and known limits.

## Dependencies

- AS-013 (the hook point lives in the tool runtime)
