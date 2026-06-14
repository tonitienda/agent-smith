---
id: AS-021
title: TUI skeleton — streaming chat, input, status line
status: done
github_issue: 21
depends_on: [AS-018]
area: tui
priority: P0
source: PRD.md §7.8, D6
---

# AS-021 · TUI skeleton

**Status: done** — Bubble Tea skeleton in `internal/tui`, wired in `cmd/smith`; framework choice in [ADR-0001](../../design/adr-0001-tui-framework.md).

## Description

The flagship face (§7.8). First slice: a working interactive chat over the agentic loop.

- Framework: Bubble Tea (+ Lipgloss) recommended — the de-facto Go TUI stack; document the choice in a short ADR.
- Streaming assistant output rendered incrementally; markdown rendering for final messages.
- Multi-line input editor with history; Esc cancels the in-flight turn.
- Status line: current model/provider, session name, spinner while working (plain text for now — the Matrix personality layer is explicitly fast-follow, D6).
- Scrollback through the transcript.
- Consumes only the loop's face-agnostic UI events (AS-018) — no business logic in the TUI.

## Acceptance criteria

- [x] `smith` launches into an interactive session; a full multi-tool turn streams visibly. (Streaming text, reasoning, and tool start/finish fold into the transcript from the loop's `UIEvent` stream; verified by unit tests and a pty smoke test of launch + render + input.)
- [x] Esc cancels cleanly mid-turn; the session remains usable. (Esc cancels the in-flight turn's context; `busy` clears on `turnDoneMsg` and input stays live — covered by `TestEscCancelsInFlightTurn`.)
- [x] Resize, scrollback, and long-output rendering don't glitch. (Scrollback/resize handled by the Bubbles `viewport`; markdown wrap width is rebuilt and caches invalidated on `WindowSizeMsg`; auto-scroll sticks to the bottom only when already there.)
- [x] TUI package contains no provider or tool imports. (Enforced by `no_business_imports_test.go`.)

## Out of scope (follow-on)

- Permission prompts, diff review, and tool-output transparency land in **AS-024**; the skeleton runs tools under the runtime's default allow-all policy.
- Context meter (**AS-025**) and `/cost` (**AS-020**) consume the same event stream and the loop `Result`; not surfaced here.
- Status-line personality/theming is the Matrix layer (**AS-053**, D6); the status line here is plain text.

## Dependencies

- AS-018 (loop + UI events)
