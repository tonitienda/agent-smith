---
id: AS-100
title: Add quick, full, architecture, and CI-local harness scripts
status: done
github_issue: 183
depends_on: [AS-099]
area: quality
priority: P1
source: docs/projects/harness-quality-system.md
---

# AS-100 · Add quick, full, architecture, and CI-local harness scripts

**Status: done**

## Description

Add thin repository-owned scripts under `scripts/harness/` so Claude hooks, Codex pre-submit behavior, local Git hooks, humans, and CI all call the same commands.

## Acceptance criteria

- [x] `scripts/harness/full.sh` wraps `./scripts/agent-quality-gate.sh` without changing its semantics.
- [x] `scripts/harness/quick.sh` runs formatting plus a documented fast deterministic test subset suitable for agent inner-loop use.
- [x] `scripts/harness/arch.sh` runs architecture/package-boundary checks directly.
- [x] `scripts/harness/ci-local.sh` runs the local approximation of CI in documented job order.
- [x] Scripts print each command before running it and preserve useful exit codes (shared `scripts/harness/_lib.sh`).
- [x] Scripts write a concise ignored artifact summary under `.cache/harness/`.

## Dependencies

- AS-099 (harness command contract)
