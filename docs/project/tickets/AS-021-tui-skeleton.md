---
id: AS-021
title: TUI skeleton — streaming chat, input, status line
status: ready-to-implement
github_issue: 21
depends_on: [AS-018]
area: tui
priority: P0
source: PRD.md §7.8, D6
---

# AS-021 · TUI skeleton

**Status: ready to implement**

## Description

The flagship face (§7.8). First slice: a working interactive chat over the agentic loop.

- Framework: Bubble Tea (+ Lipgloss) recommended — the de-facto Go TUI stack; document the choice in a short ADR.
- Streaming assistant output rendered incrementally; markdown rendering for final messages.
- Multi-line input editor with history; Esc cancels the in-flight turn.
- Status line: current model/provider, session name, spinner while working (plain text for now — the Matrix personality layer is explicitly fast-follow, D6).
- Scrollback through the transcript.
- Consumes only the loop's face-agnostic UI events (AS-018) — no business logic in the TUI.

## Acceptance criteria

- [ ] `smith` launches into an interactive session; a full multi-tool turn streams visibly.
- [ ] Esc cancels cleanly mid-turn; the session remains usable.
- [ ] Resize, scrollback, and long-output rendering don't glitch (manual test checklist in the PR).
- [ ] TUI package contains no provider or tool imports.

## Dependencies

- AS-018 (loop + UI events)
