---
id: AS-007
title: Session persistence — save, list, and load sessions on disk
status: ready-to-implement
github_issue: 7
depends_on: [AS-005]
area: core-log
priority: P0
source: PRD.md §7.9, D6
---

# AS-007 · Session persistence

**Status: ready to implement**

## Description

Sessions must survive process exit and be resumable (§7.9). Because the session *is* the append-only log (AS-005), persistence is continuous appending plus session metadata and discovery.

- Session directory layout (e.g., `~/.agent-smith/sessions/<project-hash>/<session-id>/`): event log + metadata file (project path, created/updated timestamps, title, model(s) used, totals).
- Sessions are written as they happen (append-on-event), not on exit — a crash loses at most the in-flight event.
- List sessions for the current project (id, title, age, size); load a session by ID into a live log + projection.

## Acceptance criteria

- [ ] Killing the process at any point loses no completed events.
- [ ] A loaded session produces a projection identical to the one at save time.
- [ ] Sessions are scoped/discoverable per project directory.
- [ ] Metadata is duplicated nowhere — totals derivable from the log are computed, not stored as truth.

## Dependencies

- AS-005 (event log store)
