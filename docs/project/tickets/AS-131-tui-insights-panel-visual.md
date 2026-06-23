---
id: AS-131
title: TUI /insights panel visual redesign — stat cards, timeline, tool histogram
status: ready-to-implement
github_issue: null
depends_on: [AS-121, AS-045]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.7
---

# AS-131 — /insights panel visual redesign

## Problem

AS-045 built a functional `/insights` retrospective. The current rendering is plain text.
The spec (§7.7) defines a structured dashboard with stat cards, a step timeline, and a
tool-call histogram — the visual that lets a user feel the weight of what just happened.

## What to build

### Stat grid (4 cards)

A 2×2 grid of inset cards (`BgInset` background, `ColorBorder` border):

```
┌──────────────────┐  ┌──────────────────┐
│  TURNS           │  │  TOKENS          │
│  14              │  │  86,431          │
└──────────────────┘  └──────────────────┘
┌──────────────────┐  ┌──────────────────┐
│  COST            │  │  WALL TIME       │
│  $0.0312         │  │  4 min 22 s      │
└──────────────────┘  └──────────────────┘
```

- Card label: uppercase, `StyleMuted`, small.
- Card value: `StyleCost` (`ColorCommand`) for cost; `StyleRunning` (`ColorAmberBright`)
  for wall time; `ColorFgDefault` for turns and tokens.

### "What happened" timeline

A vertical list of steps with state glyphs:

```
✓  scoped        identified the bug scope in 2 files
✓  reproduced    confirmed spinner hangs on Esc during tool call
◐  diagnosed     narrowed to missing tick-cancel on interrupt
○  fix           pending
○  verify        pending
```

- `✓` in `StyleSuccess`, `◐` in `StyleNeutral`, `○` in `StyleDim`.
- Step name: bold, `ColorFgDefault` (done) / `StyleMuted` (pending).
- Description: `StyleMuted`.

### Tool-call histogram

```
read_file    ▇▇▇▇▇▇▇▇▇▇▇▇  24
write_file   ▇▇▇▇▇          10    ← amber (write = change)
bash         ▇▇▇             6
search       ▇▇              4
```

- Bars use `▇` characters scaled to the max count.
- Colour: step down the green ramp by call frequency (most frequent = `ColorBrand`,
  least = `ColorDimmest`).
- `write_file` / `edit` / any mutating tool: bar in `ColorAmberMuted` so writes read
  distinctly.
- Count right-aligned in `StyleNeutral`.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- `/insights` renders the 4-card stat grid, timeline, and histogram.
- Cost card uses `ColorCommand`; wall-time card uses `ColorAmberBright`.
- Histogram bars scale correctly to terminal width.
- Mutating tool rows render in amber.
- Panel works with zero data (all-zero session): shows `—` placeholders in stat cards,
  empty timeline and histogram with a `no tool calls this session` dim hint.
