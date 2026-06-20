---
id: AS-084
title: Rewind file-system snapshot & restore
status: ready-to-implement
github_issue: 143
depends_on: [AS-037]
area: commands
priority: P2
source: PRD.md §7.16
---

# AS-084 · Rewind file-system snapshot & restore

**Status: ready to implement** — spun out of AS-037, whose v1 scope decision rewinds *conversation state only*.

## Description

AS-037 `/rewind` reverts the conversation projection but deliberately leaves the working tree alone — it only *warns* (in the preview) about files modified after the checkpoint. This ticket is the follow-up that would actually snapshot and restore those file changes so a rewind can put the workspace back too.

The mechanism is open: per-checkpoint content snapshots of touched files, a shadow git stash/worktree, or reconstruction from the tool-call log (the `write`/`edit` blocks already carry enough to reverse-apply, but `shell` mutations don't). It must stay reversible (re-applying the dropped changes after an undo) and honour the no-data-loss guardrail (§6) — a restore never silently clobbers uncommitted work the user did by hand.

## Clarified implementation decisions

- **Snapshot mechanism:** use content snapshots of files that Smith file tools write/edit, captured before mutation and referenced from the event log. Do not rely on git being present. Shell-driven changes remain warning-only unless the shell tool can identify touched paths in a later ticket.
- **Storage/pruning:** store snapshots under Smith's session data directory, content-addressed/deduplicated where practical, with a conservative size cap that skips very large files and reports the skip in the rewind preview. Snapshots are retained with the session and pruned by the same retention policy.
- **Conflict handling:** never silently clobber. If the file changed since the snapshot by something outside Smith, the restore preview marks a conflict and requires explicit user choice; V1 may refuse conflicted restore rather than implement merge.
- **Scope/UX:** file restore is opt-in via an explicit flag/action (`/rewind --restore-files` or equivalent) layered on the existing preview/apply confirmation. Conversation-only rewind remains the default.

## Dependencies

- AS-037 (`/rewind` checkpoints, preview, modified-files detection).
