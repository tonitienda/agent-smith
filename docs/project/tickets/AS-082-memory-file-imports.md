---
id: AS-082
title: Memory file @import-style includes
status: done
github_issue: 129
depends_on: [AS-032]
area: capability
priority: P2
source: PRD.md §7.3
---

# AS-082 · Memory file `@import` includes

**Status: ready to implement**

## Description

Spun out of AS-032 (memory files), which loads AGENTS.md / AGENT.md / CLAUDE.md
hierarchically but explicitly punts `@import`-style includes. This ticket adds
the include directive so a memory file can pull in another file's content
(Claude Code's `@path/to/file` convention), keeping the portability thesis: a
project that uses imports in its CLAUDE.md works in Agent Smith unmodified.

- Honor `@<path>` includes inside a discovered memory file, resolved relative to
  the including file's directory (and the home `~` shorthand).
- Bound recursion: a cycle (A imports B imports A) must terminate, and an import
  depth limit caps runaway nesting; a missing import degrades to a visible note,
  not a hard failure.
- Imported content enters the projection attributed to its own source file (so
  `/context` still shows where each chunk came from), not folded into the
  importer's segment.

## Acceptance criteria

- [x] A memory file with `@other.md` includes that file's content, attributed to
      `other.md` in `/context`.
- [x] Cyclic and deeply-nested imports terminate within a documented depth limit.
- [x] A missing/unreadable import surfaces a note and does not abort session start.

## Implementation notes

- A directive is a line whose only content is `@<path>` (single token); a stray
  `@` mid-sentence is left alone. Paths resolve relative to the including file's
  directory, with `~`/`~/...` expanding to the home dir.
- The imported file becomes its own `memory.Block` attributed to its own path, so
  `/context` keeps the source attribution; the importer's own segment is left
  intact (the directive line is not stripped from it).
- Termination: imports are deduplicated across a load by absolute path (a file
  pulled in twice loads once, cycles stop) and nesting is bounded by
  `memory.MaxImportDepth` (8). A missing/unreadable import degrades to a visible
  `[memory import not found: …]` note block instead of failing the load.

## Dependencies

- AS-032 (memory file discovery + loading, the loader this extends)
