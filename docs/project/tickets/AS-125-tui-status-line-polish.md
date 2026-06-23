---
id: AS-125
title: TUI status line + mode bar visual polish — spec-compliant layout and colours
status: ready-to-implement
github_issue: 390
depends_on: [AS-121, AS-025, AS-073]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.2, §2 (BgModeBar, BgStatusLine tokens)
---

# AS-125 — TUI status line + mode bar visual polish

## Problem

The status line (`statusBarStyle`) uses ANSI 15/8 (white on grey) and the mode bar
(`modeBarStyle`) uses ANSI 0/6 (black on cyan). Neither matches the spec:

- Status line: `BgStatusLine` `#16201a`, `fg.default` `#c4e3cd`.
- Mode bar: `BgModeBar` `#103a22`, mode-bar accent text `#7dffa8` / `#bdf0cf`.

The status line layout is also incomplete per §7.2:
- Left: `provider · model · session-id · goal` (goal in `ColorAmberMuted` if set,
  otherwise omitted).
- Right: context bar (`█░` + `used/total %`) · cost (`StyleCost`) · live spinner +
  `(Esc to cancel)` during a running turn.

The "alive pulse" effect (opacity 0.55 → 1.0, 2.4 s) is a colour-interpolation effect
that can be approximated in a terminal by toggling between `ColorNeutral` and
`ColorFgDefault` on the status provider text; this approximation is acceptable.

## What to build

### Status line

After AS-121 tokens are in place, update `statusLine` in `model.go`:

1. Apply `BgStatusLine` background and `ColorFgDefault` foreground via named styles.
2. Left segment: `{provider} · {model} · {session}` in `StyleNeutral`; if a goal is set,
   append ` · {goal text}` in `StyleGoal` (`ColorAmberMuted`).
3. Right segment:
   - Context bar: `█` * filled + `░` * empty (8 chars wide) + ` {used}/{total} {pct}%`
     in `StyleNeutral`. Fill colour `ColorBrand`, empty colour `ColorDim`.
   - Cost: ` $0.0032` in `StyleCost` (`ColorCommand`).
   - If a turn is running: amber braille spinner frame + ` (Esc to cancel)` in
     `StyleRunning`.
4. Separate left and right with padding to fill terminal width (existing gap logic is
   fine).

### Mode bar (Coding Mode, AS-073)

- Background: `BgModeBar` (`#103a22`).
- Mode name text: `#bdf0cf`.
- Phase names: `#4f8a64` for idle phases, `#7dffa8` for the active phase.
- Active phase wrapped in `[brackets]`.
- Right-aligned key hint `Ctrl+G m` in `#2f7a4c`.

### "Alive pulse" approximation

Toggle the provider/model text on the left between `ColorNeutral` and `ColorFgDefault`
every ~1.2 s (half the 2.4 s spec period) when the session is idle. This requires no
new tick if the caret blink tick from AS-122 is reused. Stop the toggle during an active
turn (the spinner already signals activity).

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- Status line shows the dark green background and correct phosphor text colours.
- Mode bar (in Coding Mode) shows dark green background with phase track.
- Context bar fills correctly at known token counts (verify with an existing unit test).
- No visual regressions on the existing panel tests.
