---
id: AS-180
title: Desktop home, recent workspaces, and session resume
status: ready-to-implement
github_issue: null
depends_on: [AS-178, AS-007, AS-064]
area: faces
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-180 · Desktop home, recent workspaces, and session resume

**Status: ready to implement**

## Description

Build the desktop home screen and navigation model around recent workspaces and
resumable sessions. This is the minimum product layer that makes the desktop app
feel meaningfully better than launching Smith manually each time.

## Scope

- Home screen with recent workspaces and recent sessions.
- Workspace picker / opener.
- Session list filtered by workspace.
- Resume an existing session into the main interactive view.
- Show lightweight metadata such as title and last activity time.

## Acceptance criteria

- [ ] A user can open the desktop app and start from a recent workspace.
- [ ] Existing sessions are listed and can be resumed from the home screen.
- [ ] Session navigation reuses the existing Smith persistence model.
- [ ] The app clearly distinguishes "new session" from "resume session".
- [ ] The home screen remains responsive with a realistic number of recent
      sessions.

## Non-goals

- Search across all transcripts.
- Background workboard/task orchestration.

## Dependencies

- AS-178, AS-007, AS-064.
