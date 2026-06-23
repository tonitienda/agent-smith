---
id: AS-124
title: TUI tool card visual polish — bordered cards, left rule, truncation, elapsed time
status: ready-to-implement
github_issue: null
depends_on: [AS-121, AS-024]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.2, §6
---

# AS-124 — TUI tool card visual polish

## Problem

Tool-call cards in the transcript (AS-024) render tool name and output but don't match
the spec's card layout. Missing:
- Status row with `✓` / amber braille spinner + tool name + args preview + elapsed time.
- Indented output block with a left `│` rule coloured `ColorBorder` (idle) /
  `ColorBorderActive` (running).
- Output truncation: show at most N lines with `… +M more lines — Ctrl+G t to expand`
  in `StyleDim` when the output exceeds the threshold.
- Amber spinner that stops (replaced by green `✓`) when the call succeeds.

## What to build

### Status row

```
⣾ read_file  path/to/foo.go  1.3 s
```

- Spinner glyph (`⣾⣽⣻⢿⣿⡿⣟⣯` cycle, `StyleRunning` / `ColorAmberBright`) while running;
  `✓` in `StyleSuccess` when done.
- Tool name: `StyleToolName` (`ColorNeutral`).
- Args preview (first 60 chars of the first arg value): `StyleToolArgs` (`ColorMuted`).
- Elapsed: right-aligned in `StyleMuted`; update on each tick while running.

### Output block

```
│ <output line 1>
│ <output line 2>
│ …
│ … +12 more lines — Ctrl+G t to expand
```

- Left `│` rule character in `ColorBorderActive` while running, `ColorBorder` when done.
- Output text: `StyleToolOutput` (`ColorDimmest`).
- Truncation threshold: 6 lines by default (configurable as a constant).
- The `… +N more lines` row uses `StyleDim` and references the expand hotkey.
- When the panel expand hotkey is pressed, the full output is shown (integrate with the
  existing panel framework from AS-067 if applicable; otherwise just toggle a `expanded`
  flag on the card state).

### Running border (the `border.active` signal)

While a tool call is in-flight, wrap the card (status row + output block) in a Lipgloss
box with `border` set to the left-only `│` in `ColorBorderActive`. When done, switch to
`ColorBorder`.

### Spinner tick

- Reuse the existing spinner tick if one is already in the model (the status-line spinner
  from AS-025). Drive both from the same tick source; don't add a second ticker.
- Cycle: `⣾ ⣽ ⣻ ⢿ ⣿ ⡿ ⣟ ⣯`, frame rate ~110 ms.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- Running a tool call shows the amber spinner, tool name, and arg preview in a bordered
  card. The elapsed time increments visibly.
- On completion the spinner becomes `✓` in green, the border dims, and the elapsed time
  freezes.
- Output longer than 6 lines is truncated with the `… +N more lines` hint.
- The card is visually static once the call is done — no ticks on completed content.
