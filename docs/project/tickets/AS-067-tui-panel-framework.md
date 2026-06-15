---
id: AS-067
title: TUI inspect-mode panel framework + focus/hotkey routing
status: done
github_issue: 104
depends_on: [AS-021, AS-022]
area: tui
priority: P0
source: TUI-UX.md (D-TUI-3,-4,-7,-8,-10,-11,-12), UX.md §4.3/§9.4
---

# AS-067 · TUI inspect-mode panel framework + focus/hotkey routing

**Status: done**

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

## Implementation notes (landed)

- **Panel host (D-TUI-3).** The existing full-screen panel now pins the status
  line above the body, with a footer keybar below; Esc returns to work mode and
  the in-flight turn keeps streaming (events still apply while a panel is open).
- **Open routing / hotkeys (D-TUI-4, settles open question 1).** A leader chord
  `Ctrl+G` then a key opens a panel: `c`→`/context`, `d`→`/diff`, `h`→`/help`,
  `$`→`/cost`. Bindings for panels not yet built (`/context`, `/diff`) are
  harmless no-ops until their command registers. Both paths dispatch through the
  shared registry, identical to the palette.
- **Focus model (D-TUI-7).** Bare letters always reach the prompt; the only key
  that captures the next keystroke is the leader (`Ctrl+G`), and an unbound
  selector cancels the chord without typing.
- **Modal overlay (D-TUI-8).** `modal` is a reusable sub-model (`openModal`):
  focus trapped, severe red styling, verbatim detail (UX.md §11), arrows to
  choose, Enter to confirm, Esc to deny by default. AS-024's destructive-prompt
  path constructs one and supplies the `decide` callback.
- **Startup header (D-TUI-10).** Small ASCII banner + `project · model · mode`
  at the top of the scrollback; on by default, hidden by `--no-splash`
  (`tui.WithoutSplash`) and, once it lands, serious mode.
- **Graceful degrade (D-TUI-11, settles open question 3).** The status line is
  the first chrome dropped, below `inputHeight + statusHeight + 1` rows, in both
  work and inspect modes — no "terminal too small" block.
