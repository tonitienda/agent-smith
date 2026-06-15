---
id: AS-064
title: "/resume interactive picker + transcript rehydration"
status: ready-to-implement
github_issue: 97
depends_on: [AS-023, AS-024]
area: tui
priority: P2
source: AS-023 follow-on
---

# AS-064 · /resume interactive picker + transcript rehydration

**Status: ready to implement**

## Description

Spun out of AS-023 (parity commands). AS-023 shipped `/clear`, `/model`, and
`/resume` with a deliberately minimal `/resume` UX: `/resume` lists the project's
sessions and `/resume <id>` loads one by ID, and after a resume the on-screen
transcript starts fresh (the engine's *projection* of the loaded log is exact —
that is what AS-023's AC tests — but the TUI scrollback is not replayed).

This ticket closes the two UX gaps that were punted, now that the diff/transcript
rendering surface (AS-024) gives a place to hang rehydrated history:

- **Interactive picker.** `/resume` with no arg opens a full-screen, keyboard-
  navigable list (the same multi-select affordance as `/context`, AS-026) so a
  session can be chosen and loaded with Enter instead of copy-pasting an ID. The
  ID-argument form (`/resume <id>`, `smith --resume <id>`) stays as the scriptable
  path.
- **Transcript rehydration.** On `/clear`/`/resume` the face rebuilds its visible
  transcript from the loaded log's projected blocks (user/assistant/tool segments),
  so resuming shows the conversation, not a blank screen. This is a face-only
  concern — render projected blocks into segments — and must reuse the AS-024
  tool-call/diff rendering so a rehydrated turn looks like a live one.

## Acceptance criteria

- [ ] `/resume` with no arg opens an interactive list; Enter loads the highlighted
  session; Esc cancels without changing the active session.
- [ ] After resume, the transcript shows the loaded session's prior turns rendered
  the same way they were live (text, tool calls, diffs).
- [ ] The ID forms (`/resume <id>`, `smith --resume <id>`) still work unchanged.
- [ ] Rehydration is pure projection (no model calls) and the post-resume meter
  matches the restored session's last live state.

## Dependencies

- AS-023 (the `/resume` command + session swap seam), AS-024 (tool-call/diff
  rendering to reuse when replaying historical turns)
