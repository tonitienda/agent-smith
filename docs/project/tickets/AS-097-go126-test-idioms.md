---
id: AS-097
title: Modernize tests with Go 1.26 stdlib idioms
status: done
github_issue: 167
depends_on: []
area: quality
priority: P2
source: code-improvements.md
---

# AS-097 · Modernize tests with Go 1.26 stdlib idioms

**Status: ready to implement**

## Description

Audit tests for modern Go 1.26 standard-library idioms. Focus especially on
streaming, subprocess, HTTP, runtime, and concurrency tests where cleanup and
synchronization bugs are easy to miss.

Use `t.Context()` where cancellation-aware tests launch subprocesses, streams, or
HTTP requests. Prefer `t.TempDir()` and `t.Setenv()` over manual cleanup. Use
stdlib `cmp`, `slices`, `maps`, and deterministic synchronization helpers where
they make tests clearer.

## Acceptance criteria

- [ ] Cancellation-aware tests in provider/MCP/tool/runtime areas use
      `t.Context()` where appropriate.
- [ ] Manual temp directory and environment cleanup is replaced with `t.TempDir`
      and `t.Setenv` where practical.
- [ ] Repetitive custom comparison loops are simplified with stdlib helpers when
      readability improves.
- [ ] The full quality gate passes after the cleanup.
- [ ] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure.

## Dependencies

- None
