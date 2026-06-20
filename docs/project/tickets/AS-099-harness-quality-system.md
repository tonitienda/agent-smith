---
id: AS-099
title: Harness quality system design and command contract
status: done
github_issue: 182
depends_on: [AS-095, AS-098]
area: quality
priority: P1
source: docs/projects/harness-quality-system.md
---

# AS-099 · Harness quality system design and command contract

**Status: ready to implement**

## Description

Turn the harness design into a concrete repository contract for humans and agents. The contract should define the canonical local commands, their relationship to CI, when agents should run quick vs full checks, and how failures are reported.

## Acceptance criteria

- [ ] `docs/projects/harness-quality-system.md` is reviewed and linked from `docs/agent-quality-gates.md`.
- [ ] The docs define quick, full, architecture, and CI-local harness entry points and when to use each.
- [ ] The docs include a local-CI parity table mapping each CI job to a local command.
- [ ] Agent guidance in `CLAUDE.md`/`AGENTS.md` references the harness contract instead of only naming the full gate.
- [ ] The final response format for agents remains compatible with the repository testing-summary convention.

## Dependencies

- AS-095 (dependency boundary definitions and tests)
- AS-098 (architecture contract documentation and tests)
