---
id: AS-127
title: TUI command palette visual redesign — search field, per-command styling, footer hints
status: ready-to-implement
github_issue: null
depends_on: [AS-121, AS-022]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.6
---

# AS-127 — Command palette visual redesign

## Problem

AS-022 built a functional command palette (slash-triggered fuzzy list). The current
rendering uses raw ANSI colours and no border. The spec (§7.6) defines a distinct visual
that makes the palette read as a first-class modal surface.

## What to build

### Search field row

```
❯ /context█
─────────────────
```

- `❯` in `ColorBrand`, input text in `StyleSlashCommand`, block cursor `█` in
  `ColorBrand`.
- Thin horizontal rule below (`ColorBorderSelect`) spanning the palette width.
- Right-aligned `N commands` count in `StyleDim`.

### Match list

Each row:

```
  /context      show context window breakdown
  /serious      mute theme
```

- Command name: `StyleSlashCommand` (`ColorCommand`) for normal commands; `/serious`
  specifically rendered in `StyleRunning` (`ColorAmberBright`) so it reads as a
  "danger/toggle" action.
- Description: `StyleMuted` (`ColorMuted`).
- Selected row: background `BgModeBar`, leading `❯` in `ColorBrand`, command in
  `ColorFgDefault`.
- Non-selected rows: no background, no caret, command in `ColorCommand`.

### Footer hint row

```
↑↓ move · ↵ run · tab complete · esc close
```

Rendered in `StyleDim` below the last match row.

### Outer border

Wrap the entire palette (search row + list + footer) in a Lipgloss border using
`ColorBorderSelect` for the active border colour and `ColorBorder` when no item is
selected (i.e., input is empty).

### Tab completion

When the user presses Tab with exactly one match, fill the input with the command name
(existing behaviour from AS-022); update the search field render to show the completed
text in `StyleSlashCommand`.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- Typing `/` opens the palette with the search border, match list, and footer hints.
- Selected row shows `BgModeBar` background and `❯`.
- `/serious` entry renders in amber.
- Footer hint row visible at the bottom of the palette.
