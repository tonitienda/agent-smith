---
id: AS-179
title: Desktop tool activity and permission rail
status: ready-to-implement
github_issue: null
depends_on: [AS-178, AS-024, AS-016]
area: faces
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-179 · Desktop tool activity and permission rail

**Status: ready to implement**

## Description

Add the visibility features that make Smith feel trustworthy in a desktop form:
tool activity, tool results, and first-class permission prompts for ask-mode
actions.

The experience should feel closer to a reviewable operations rail than a hidden
debug console.

## Scope

- Render tool-call/tool-result activity in a dedicated rail or pane.
- Show command/args, file-edit summaries, and truncated output where available.
- Render ask-mode permission prompts with allow / allow-always / deny.
- Keep the UI in sync with ongoing turn state.

## Acceptance criteria

- [ ] Tool activity is visible while a turn is running.
- [ ] Ask-mode permission requests appear as first-class desktop UI, not raw
      transport events.
- [ ] User choices on a permission prompt are sent back to Smith correctly.
- [ ] File-edit and shell-style actions are distinguishable in the activity UI.
- [ ] The implementation preserves AS-024/AS-016 semantics rather than creating
      desktop-only variants.

## Non-goals

- Full `/context` or `/cost` views.
- Multi-session coordination.

## Dependencies

- AS-178, AS-024, AS-016.
