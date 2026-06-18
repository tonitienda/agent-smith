---
id: AS-084
title: Rewind file-system snapshot & restore
status: needs-clarification
github_issue: null
depends_on: [AS-037]
area: commands
priority: P2
source: PRD.md §7.16
---

# AS-084 · Rewind file-system snapshot & restore

**Status: needs clarification** — spun out of AS-037, whose v1 scope decision rewinds *conversation state only*.

## Description

AS-037 `/rewind` reverts the conversation projection but deliberately leaves the working tree alone — it only *warns* (in the preview) about files modified after the checkpoint. This ticket is the follow-up that would actually snapshot and restore those file changes so a rewind can put the workspace back too.

The mechanism is open: per-checkpoint content snapshots of touched files, a shadow git stash/worktree, or reconstruction from the tool-call log (the `write`/`edit` blocks already carry enough to reverse-apply, but `shell` mutations don't). It must stay reversible (re-applying the dropped changes after an undo) and honour the no-data-loss guardrail (§6) — a restore never silently clobbers uncommitted work the user did by hand.

## Open questions

- Snapshot mechanism: content copies, git-backed shadow, or log replay? How are `shell`-driven changes (not parsed by AS-037's warning) captured?
- Storage/cost budget for snapshots on large files; when are they pruned?
- Conflict handling when the working tree changed since the checkpoint (hand edits, external tools) — prompt, refuse, or three-way merge?
- Scope: opt-in flag vs default; does it ride the same `/rewind --apply` confirm or a separate `--restore-files`?

## Dependencies

- AS-037 (`/rewind` checkpoints, preview, modified-files detection).
