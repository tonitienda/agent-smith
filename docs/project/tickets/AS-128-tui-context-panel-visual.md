---
id: AS-128
title: TUI /context panel visual redesign — segmented bar, legend grid, stats rail
status: ready-to-implement
github_issue: null
depends_on: [AS-121, AS-026]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.3
---

# AS-128 — /context panel visual redesign

## Problem

AS-026 built a functional `/context` panel. The current rendering is a plain text
breakdown. The spec (§7.3) defines a rich token-budget dashboard as one of the
"flagship wedge" visuals — the most demo-worthy panel in the product.

## What to build

### Segmented bar

A single horizontal bar spanning the panel width, divided into coloured segments
proportional to each category's token count:

| Category | Colour |
|---|---|
| System prompt | `#00ff66` |
| Tool definitions | `#00cc52` |
| Project memory (CLAUDE.md) | `#caa24a` |
| Message history | `#7dffa8` |
| File contents | `#3f8a5a` |
| Subagent results | `#2f6f4a` |
| Free | `#12211a` |

- Render using `█` characters (one per N tokens, scaled to terminal width).
- **Amber vertical marker** (`│` in `ColorAmberBright`) at the auto-compact threshold
  (90% / 180 k by default), labelled `auto-compact 180k` above the bar in
  `StyleRunning`.
- Scale labels below the bar: `0` (left) · `{used} / {total} used · {pct}%` (centre) ·
  `{total}` (right), all in `StyleMuted`.

### Legend grid (2 columns)

Below the bar, a 2-column grid: coloured swatch `█` · category name · token count ·
percentage. Rendered in `StyleNeutral` / `StyleMuted` alternating for name/value.

Add a `CLAUDE.md loaded · N rules ✓` row at the bottom of the legend in `StyleSuccess`.

### Tip line

```
tip: /compact to reclaim tokens
```

In `StyleDim`, below the legend.

### Stats rail (inset card)

A right-aligned or bottom card (background `BgInset`) with:

- Window size, used, free (tokens).
- Cache read / write / hit-rate.
- Session cost — large, `StyleCost` (`ColorCommand`).

Use a Lipgloss box with `BgInset` background and `ColorBorder` border.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- `/context` renders the segmented bar with correct proportional widths at multiple
  terminal widths (80, 120, 200 cols).
- Amber auto-compact marker appears when `used > 0.9 * window`.
- Stats rail shows cost in `ColorCommand`.
- Panel degrades gracefully when token data is unavailable (shows `—` placeholders).
