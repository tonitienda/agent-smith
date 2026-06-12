---
id: AS-025
title: Always-visible context meter in the TUI
status: ready-to-implement
github_issue: 25
depends_on: [AS-006, AS-020, AS-021]
area: tui
priority: P0
source: PRD.md §7.8, §5
---

# AS-025 · Always-visible context meter

**Status: ready to implement**

## Description

§7.8 requires the context meter to be always visible — the constant, ambient version of the `/context` deep-dive. It is the first taste of the observability wedge.

- Status-line widget: tokens used / window size (per current model), percentage bar, session $ so far.
- Live vs excluded split: how many tokens `/clean` operations have reclaimed (once AS-028 lands, this becomes visible value).
- Color thresholds (e.g., green < 60%, yellow < 85%, red beyond) and an indicator when nearing the model's window limit.
- Updates after every event append — driven by projection metadata (AS-006) + block token counts (AS-020), no extra model calls.

## Acceptance criteria

- [ ] Meter is visible at all times in a session and updates within one event of any change.
- [ ] Switching models (`/model`) rescales the window denominator immediately.
- [ ] Numbers agree with `/cost` and (later) `/context` — single accounting source, no drift.
- [ ] Zero model calls and no measurable input latency from the meter.

## Dependencies

- AS-006 (projection metadata), AS-020 (token counts), AS-021 (status line)
