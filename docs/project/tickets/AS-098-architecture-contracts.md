---
id: AS-098
title: Document and test package architecture contracts
status: ready-to-implement
github_issue: null
depends_on: [AS-095]
area: architecture
priority: P2
source: code-improvements.md
---

# AS-098 · Document and test package architecture contracts

**Status: ready to implement**

## Description

The repository has strong product docs and ticket guidance, but package-level
architecture rules should be explicit for humans and agents. Add a concise docs
note that describes dependency direction and ownership for schema, event log,
projection, provider contracts, concrete providers, loop, tools, faces, and
`cmd/*` composition roots.

Back the most important contracts with lightweight tests so architectural drift
is caught early.

## Acceptance criteria

- [ ] A docs page records package dependency direction and ownership rules.
- [ ] Tests assert that provider contracts do not import concrete providers,
      concrete providers do not import loop/TUI/cmd, and loop does not import
      face packages.
- [ ] The note explains where to put new code for commands, faces, providers,
      tools, and application wiring.
- [ ] AGENTS/CLAUDE guidance is updated if new rules affect future agent work.

## Dependencies

- AS-095 (dependency boundary definitions and tests)
