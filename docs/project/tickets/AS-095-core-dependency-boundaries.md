---
id: AS-095
title: Enforce stdlib-first dependency boundaries for core packages
status: done
github_issue: 165
depends_on: []
area: quality
priority: P2
source: code-improvements.md
---

# AS-095 · Enforce stdlib-first dependency boundaries for core packages

**Status: done**

## Description

External dependencies are appropriate at face boundaries, especially the TUI.
Core packages should remain stdlib-first unless a ticket explicitly justifies an
exception. Some import-boundary tests already exist; extend that idea to the
architectural core.

Define which packages are core (`schema`, event log, projection, provider
contracts, loop, cost, config, permissions, tools, and similar) and add tests
that prevent UI/third-party dependencies from leaking into them.

## Acceptance criteria

- [x] A documented dependency boundary identifies core, provider adapter, face,
      and command/executable layers.
- [x] Import-boundary tests fail if core packages import TUI libraries or other
      unapproved third-party dependencies.
- [x] Existing allowed exceptions are documented explicitly.
- [x] No new runtime dependencies are introduced.
- [x] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure.

## Dependencies

- None
