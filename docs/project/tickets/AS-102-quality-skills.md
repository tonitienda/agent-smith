---
id: AS-102
title: Repository skills for quality gates and CI triage
status: done
github_issue: 185
depends_on: [AS-099, AS-100]
area: capability
priority: P2
source: docs/projects/harness-quality-system.md
---

# AS-102 · Repository skills for quality gates and CI triage

**Status: done**

## Description

Create concise repository skills that help agents choose, run, and interpret the harness consistently. The skills should reference canonical docs and scripts rather than copying long instructions.

## Acceptance criteria

- [x] Add a `quality-gate-runner` skill for selecting quick/full/arch checks, interpreting failures, and reporting environment warnings.
- [x] Add a `ci-failure-triage` skill for mapping CI failures to local harness commands and producing a reproduction plan.
- [x] Add or update a ticket-start workflow skill that reminds agents to read tickets, dependencies, PRD decisions, and architecture docs before editing (`ticket-implementer`).
- [x] Skills are documented for Claude, Codex, and future Smith skill loading where applicable (see the Repository skills section of `docs/agent-quality-gates.md`).
- [x] Skill instructions stay short and link back to `docs/projects/harness-quality-system.md` and `docs/agent-quality-gates.md`.

## Dependencies

- AS-099 (harness command contract)
- AS-100 (harness scripts)
