---
id: AS-067
title: TUI inspect-mode panel framework + focus/hotkey routing
status: ready-to-implement
github_issue: null
depends_on: [AS-021, AS-022]
area: tui
priority: P0
source: TUI-UX.md (D-TUI-3,-4,-7,-10,-12), UX.md §4.3/§9.4
---

# AS-067 · TUI inspect-mode panel framework + focus/hotkey routing

**Status: ready to implement**

## Description

AS-021 shipped the work-mode transcript shell. The TUI now needs the generic
**inspect-mode** host that `/context` (AS-026), `/diff` (AS-024), and future
`/agents`/`/resume` panels render inside, plus the focus and hotkey rules that
tie work mode and inspect mode together — per [TUI-UX.md](../TUI-UX.md). Building
this once, before each panel reinvents it, keeps panels as reusable view models
(D-TUI-2) rather than hard-coded screens.

Scope:

- **Panel host (D-TUI-3).** Full-screen panel swap over the transcript, with the
  status line pinned. Esc returns to work mode (cooperating with the locked Esc
  cascade, UX.md §9.2) without losing the sense of what's running. Panels are
  reusable sub-models so a wide-terminal side-by-side host can render them later
  (UX.md §4.2) without a rewrite.
- **Open routing (D-TUI-4).** Panels open via the palette (`/context`, …) *and* a
  modifier/leader hotkey for the common ones — both resolving through the shared
  command registry (AS-022).
- **Focus model (D-TUI-7).** Always-typing prompt: keystrokes go to the input;
  scrolling and panel hotkeys are modifiers (PgUp / ctrl- / alt- / leader), never
  bare letters. In-TUI help via `/help` or a chord.
- **Modal overlay (D-TUI-8 support).** Blocking modal-overlay infra
  (focus trapped, severe styling) reused by AS-024's destructive permission
  prompts.
- **Startup header (D-TUI-10).** Small ASCII header (project/model/mode) on
  launch; `--no-splash` and serious-mode hide it. No model call, no delay.
- **Liveness placement (D-TUI-12).** Spinner/elapsed in the status line and
  active tool card only; never over transcript text/code/diffs.
- **Graceful degrade (D-TUI-11).** Below the layout's needs, drop status
  line/truncate panels rather than blocking; driven by `WindowSizeMsg`.

No business logic in the TUI (AS-021 import guard still holds). Bubble
Tea/Lipgloss only.

## Acceptance criteria

- [ ] Opening a panel (palette or hotkey) swaps to full screen with the status
  line pinned; Esc returns to the transcript with the in-flight turn intact.
- [ ] The prompt captures bare-letter input at all times; panel hotkeys fire only
  with their modifier.
- [ ] A modal overlay traps focus until dismissed and can be reused by a caller
  (covered by AS-024's destructive-prompt path).
- [ ] Startup header renders by default and is suppressed by `--no-splash`/serious
  mode.
- [ ] Resizing below the layout minimum degrades without glitching (extends the
  AS-021 resize AC).

## Dependencies

- AS-021 (work-mode shell, event pump, viewport), AS-022 (command registry for
  panel/hotkey dispatch). Consumed by AS-024 and AS-026.
