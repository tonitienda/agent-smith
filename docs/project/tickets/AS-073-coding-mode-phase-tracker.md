---
id: AS-073
title: Coding Mode phase tracker panel + mode presentation
status: done
github_issue: 123
depends_on: [AS-067, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-4)
---

# AS-073 · Coding Mode phase tracker panel + presentation

**Status: ready to implement**

## Description

Make entering Coding Mode **feel like crossing a threshold** (D-CODE-4), not just
a chattier prompt — while keeping the change confined to chrome. Built on the
inspect-mode panel framework (AS-067, D-TUI-3) so it reuses focus/hotkey routing.

- A pinned **phase tracker** showing the seven phases (think · analyse · plan ·
  implement · verify · refactor · reflect) with the current one highlighted.
- The active **goal** and the **artifacts produced so far** (gap note, plan,
  verification note, etc.) visible while in the mode.
- Work still happens in the transcript; the mode changes the surrounding chrome
  (status line, tracker), not the conversation flow.
- **Degrades cleanly off the TUI:** ACP/headless faces emit plain phase-tagged
  events only — no layout, no flavor (consistent with the PRD's chrome rule and
  D-CODE-4). Confirm headless scope against open question Q5.

## Acceptance criteria

- [x] Entering the mode shows the phase tracker with the current phase
      highlighted; advancing/jumping phases updates it live. (Pinned mode bar
      below the status line, fed by `Meta.PhaseTracker`; re-read each delta.)
- [x] The current goal and produced artifacts are visible while in the mode and
      reachable via the panel framework's focus/hotkey rules (AS-067). (Goal in
      the status line; `Ctrl+G m` opens the mode panel — goal, tracker, phases
      visited. Phase-produced *artifacts* are recorded by AS-076 and slot into
      `mode.Panel` when that lands; the display surface is in place.)
- [x] Exiting the mode returns to the normal work-mode shell with no residual
      chrome. (`Meta.Mode` empties on exit, so the bar drops; a test guards it.)
- [x] Non-interactive faces (`smith run`, ACP) produce phase-tagged events with
      no layout/flavor; nothing mode-specific leaks into machine-readable output.
      (Chrome lives only in the TUI status stack; commands emit plain text.)

## Clarified implementation decisions

- **Headless/ACP behavior:** Coding Mode is meaningful across faces as event/state, but the threshold-crossing presentation is TUI-only. `smith run` and ACP may enter/advance mode and emit phase-tagged events; they must not render panels or themed chrome in machine-readable output.

## Dependencies

- AS-067 (inspect-mode panel framework + focus/hotkey routing), AS-072 (mode
  shell + phase state the panel renders).
