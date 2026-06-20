---
id: AS-094
title: Standardize filesystem traversal on fs.FS and WalkDir
status: done
github_issue: 164
depends_on: []
area: architecture
priority: P2
source: code-improvements.md
---

# AS-094 · Standardize filesystem traversal on fs.FS and WalkDir

**Status: done**

Implemented: the read-only scanners in `internal/skill`, `internal/customcmd`,
and `internal/initscaffold` now do their discovery over `io/fs` — an unexported
`loadFS`/inspection helper takes an `fs.FS` and the public entry point adapts the
OS tree at the edge with `os.DirFS(root)`. `os.DirFS` bounds reads to the root,
so a scanner can no longer read across the project boundary; reads use
`fs.ReadDir`/`fs.ReadFile`/`fs.Stat` and slash paths. Recursive OS walking
(glob/grep in `internal/tool/builtin`) already uses `filepath.WalkDir`. Tests for
the three packages gained `fstest.MapFS` coverage that drives discovery with no
disk I/O, plus an explicit root-containment assertion.

## Description

Repo scanning appears in memory files, skills, custom commands, init scaffolding,
sessions, tickets, and related features. Standardize read-only traversal on the
Go standard library's `io/fs`, `fs.FS`, `fs.WalkDir`, and `filepath.WalkDir`.

Where code reads an OS tree, adapt at the edge with `os.DirFS(root)` and keep
safe relative path handling explicit. Add small local helpers only where patterns
repeat, such as nearest-file-upward search, bounded text reads, and safe relative
paths within a root.

## Acceptance criteria

- [ ] Read-only scanners in at least three packages accept or can be tested with
      `fs.FS`.
- [ ] OS-specific path walking is kept at package edges and uses `WalkDir`.
- [ ] Tests use `fstest.MapFS` or `t.TempDir` as appropriate instead of heavy
      mocks.
- [ ] Path normalization and root containment behavior is covered by tests.
- [ ] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure.

## Dependencies

- None
