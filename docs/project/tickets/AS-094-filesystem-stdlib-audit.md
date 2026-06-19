---
id: AS-094
title: Standardize filesystem traversal on fs.FS and WalkDir
status: ready-to-implement
github_issue: 164
depends_on: []
area: architecture
priority: P2
source: code-improvements.md
---

# AS-094 · Standardize filesystem traversal on fs.FS and WalkDir

**Status: ready to implement**

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

## Dependencies

- None
