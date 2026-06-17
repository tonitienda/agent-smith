---
id: AS-073
title: Coding Mode phase tracker panel + mode presentation
status: ready-to-implement
github_issue: null
depends_on: [AS-067, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-4)
---

# AS-073 · Coding Mode phase tracker panel + presentation

**Status: ready-to-implement**

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

- [ ] Entering the mode shows the phase tracker with the current phase
      highlighted; advancing/jumping phases updates it live.
- [ ] The current goal and produced artifacts are visible while in the mode and
      reachable via the panel framework's focus/hotkey rules (AS-067).
- [ ] Exiting the mode returns to the normal work-mode shell with no residual
      chrome.
- [ ] Non-interactive faces (`smith run`, ACP) produce phase-tagged events with
      no layout/flavor; nothing mode-specific leaks into machine-readable output.

## Dependencies

- AS-067 (inspect-mode panel framework + focus/hotkey routing), AS-072 (mode
  shell + phase state the panel renders).
