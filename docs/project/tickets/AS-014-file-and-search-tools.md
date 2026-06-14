---
id: AS-014
title: "Core tools: file read / write / edit, glob, grep"
status: done
github_issue: 14
depends_on: [AS-013]
area: tools
priority: P0
source: PRD.md §7.2, D6
---

# AS-014 · Core tools: file read/write/edit, glob, grep

**Status: done** — implemented in `internal/tool/builtin` (`builtin.NewFS(root).Tools()`).
`grep` is pure Go (`regexp` + a filesystem walk), no ripgrep dependency, keeping the
binary self-contained (stdlib-only, per repo conventions). `read` records its content as a
`file_read` block via a new `tool.Output.FileRead` field that the runtime turns into a
first-class `file_read` block ahead of the loop-closing `tool_result` (PRD D3). Path
escapes are rejected lexically; symlink traversal out of the root is a documented V1 limit
deferred to AS-016's security posture doc.

## Description

The minimum tool set for a credible coding agent (§7.2, D6 "file/shell tools").

- **read**: path + optional offset/limit; output recorded as a `file_read` block (the block type exists specifically so `/context` can attribute window cost to files — D3).
- **write**: create/overwrite with explicit distinction; refuses to overwrite a file never read in-session (safety convention).
- **edit**: exact string-replace semantics (old → new, uniqueness required; replace-all flag).
- **glob**: pattern matching, sorted results.
- **grep**: regex content search (bundle ripgrep or pure-Go equivalent — implementer's choice, document it).
- All file paths resolved against the session working directory; reject path escapes outside the project root unless permitted (ties into AS-016 permission rules).

## Acceptance criteria

- [x] Each tool has unit tests covering success, not-found, and permission-denied paths.
- [x] `edit` fails loudly on ambiguous (non-unique) matches.
- [x] Re-reading the same file produces a new `file_read` block (duplicate reads must stay visible — `/context` highlights them later, §7.11).
- [x] Reads of huge files truncate with an explicit marker instead of flooding the window.

## Dependencies

- AS-013 (tool runtime)
