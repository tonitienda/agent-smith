---
id: AS-025
title: Always-visible context meter in the TUI
status: done
github_issue: 25
depends_on: [AS-006, AS-020, AS-021]
area: tui
priority: P0
source: PRD.md §7.8, §5
---

# AS-025 · Always-visible context meter

**Status: done**

## Implementation notes

- A `tui.Meter` value (tokens used, window size, session cost, cost-known flag)
  plus a `tui.MeterFunc func(model string) Meter` seam keeps the face decoupled
  from the accounting engine — `cmd/smith` closes the func over the session log
  and pricing table and takes the active model per call, so a `/model` switch
  (AS-023) rescales the window denominator immediately. The TUI imports neither
  `internal/cost` nor `internal/provider` (the AS-021 boundary test still holds).
- The meter is recomputed once per loop event (cached in the model, rendered into
  the status line), so it updates within one event and adds no per-keystroke or
  model-call cost. It reads the same `cost.Summarize` source as `/cost`, so the
  session dollar figure cannot drift from the command. The closure memoizes on the
  log length and active model, so the many text-delta events in a streamed turn
  cost a single O(1) length check instead of re-summarizing the whole log.
- Window occupancy is the most recent turn's prompt+output tokens
  (`cost.TurnCost.ContextTokens`) — the figure the provider last counted — which
  needs no extra call. AS-063 (per-block estimates) will refine this into a
  composed window size. The denominator is the model's context window, added as
  an additive `context_window` field on the pricing table (`Table.Window`), so
  switching models rescales it immediately. Unknown window → bare token count;
  unpriced session → cost shown as `$?`.
- Color thresholds: green < 60%, yellow < 85%, red ≥ 85%, with a fixed-width fill
  bar. The live-vs-excluded reclaim split is left for AS-028 (`/clean`) to surface
  once exclusions exist.

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
