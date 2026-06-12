---
id: AS-037
title: /rewind — checkpoint and restore
status: ready-to-implement
github_issue: 37
depends_on: [AS-006, AS-022]
area: commands
priority: P1
source: PRD.md §7.16, D3, Appendix A
---

# AS-037 · /rewind

**Status: ready to implement**

## Description

Parity command (§7.16). D3 makes this structurally cheap: a checkpoint is just an event index, and restore is a **rewind event** that re-projects the log as of that point — history is never rewritten, so a rewind is itself undoable.

- Automatic checkpoints at every user turn; optional named manual checkpoints (`/rewind --mark "before refactor"`).
- `/rewind` opens a picker (turn list with timestamps, cost, first-line preview); selecting restores the projection to that point via an appended rewind event (provenance: user).
- Post-rewind, later blocks are excluded-but-archived — browsable in the `/context` archive view, and restorable (un-rewind).
- **Scope decision (documented):** V1 rewinds *conversation state only*. File-system changes made by tools are not reverted; the picker shows a warning listing files modified after the checkpoint. File snapshot/restore is a possible follow-up ticket.

## Acceptance criteria

- [ ] Rewinding to turn N yields a projection identical to the historical projection at turn N (golden test via point-in-time projection).
- [ ] The rewind itself is reversible; no events are deleted (§6 no-data-loss guardrail).
- [ ] Picker shows turn metadata and the modified-files warning.
- [ ] Works mid-session across provider switches (`/model`).

## Dependencies

- AS-006 (point-in-time projection), AS-022 (command framework)
