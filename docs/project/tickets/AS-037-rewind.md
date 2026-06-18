---
id: AS-037
title: /rewind — checkpoint and restore
status: done
github_issue: 37
depends_on: [AS-006, AS-022]
area: commands
priority: P1
source: PRD.md §7.16, D3, Appendix A
---

# AS-037 · /rewind

**Status: done** — engine in `internal/rewind`, command in `cmd/smith` (`/rewind`, `--mark`, `--apply`, `--undo`, `--cancel`).

## Description

Parity command (§7.16). D3 makes this structurally cheap: a checkpoint is just an event index, and restore is a **rewind event** that re-projects the log as of that point — history is never rewritten, so a rewind is itself undoable.

- Automatic checkpoints at every user turn; optional named manual checkpoints (`/rewind --mark "before refactor"`).
- `/rewind` opens a picker (turn list with timestamps, cost, first-line preview); selecting restores the projection to that point via an appended rewind event (provenance: user).
- Post-rewind, later blocks are excluded-but-archived — browsable in the `/context` archive view, and restorable (un-rewind).
- **Scope decision (documented):** V1 rewinds *conversation state only*. File-system changes made by tools are not reverted; the picker shows a warning listing files modified after the checkpoint. File snapshot/restore is a possible follow-up ticket.

## Acceptance criteria

- [x] Rewinding to turn N yields a projection identical to the historical projection at turn N (golden test via point-in-time projection). — `TestRewindMatchesPointInTimeProjection`
- [x] The rewind itself is reversible; no events are deleted (§6 no-data-loss guardrail). — `TestRewindIsReversible` (undo is a counter-exclusion; the log only grows)
- [x] Picker shows turn metadata (time, turn/mark, preview) and the preview lists the modified-files warning. — `TestModifiedFilesWarning`
- [x] Works mid-session across provider switches (`/model`): the rewind is a pure projection over the log, independent of the active provider, and `/rewind --undo` / picker re-dispatch run on whatever session is active.

## Dependencies

- AS-006 (point-in-time projection), AS-022 (command framework)
