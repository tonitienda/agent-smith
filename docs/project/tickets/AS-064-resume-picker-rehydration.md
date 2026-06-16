---
id: AS-064
title: "/resume interactive picker + transcript rehydration"
status: done
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

- [x] `/resume` with no arg opens an interactive list; Enter loads the highlighted
  session; Esc cancels without changing the active session.
- [x] After resume, the transcript shows the loaded session's prior turns rendered
  the same way they were live (text, tool calls, diffs).
- [x] The ID forms (`/resume <id>`, `smith --resume <id>`) still work unchanged.
- [x] Rehydration is pure projection (no model calls) and the post-resume meter
  matches the restored session's last live state.

## Implementation notes

- The no-arg `/resume` handler now returns both the scriptable text listing
  (Output.Text, for `smith session list`) and an additive `command.Picker`
  (Output.Picker). A non-interactive face ignores the picker; the TUI opens it
  as a single-select list bound to the originating command, so choosing an item
  re-dispatches the exact `/resume <id>` path — no new load logic.
- Transcript rehydration is a `RehydrateFunc` seam (`tui.WithRehydrate`) that
  yields the active session's projected **live** blocks. `segmentsFromBlocks`
  folds those blocks into transcript segments the same way the live loop does
  (reusing the AS-024 tool-card pairing), and `ResetView` plus a `--resume`
  launch both rebuild the visible transcript through it. Pure projection at the
  active model — no model calls — so the meter already matches.

## Dependencies

- AS-023 (the `/resume` command + session swap seam), AS-024 (tool-call/diff
  rendering to reuse when replaying historical turns)
