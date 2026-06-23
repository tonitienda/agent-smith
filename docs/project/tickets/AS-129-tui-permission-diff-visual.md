---
id: AS-129
title: TUI permission gate visual redesign — diff colours, dimmed context, option list
status: ready-to-implement
github_issue: 394
depends_on: [AS-121, AS-024]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.5
---

# AS-129 — Permission gate visual redesign

## Problem

AS-024 built functional permission prompts and diff display. The current rendering uses
plain ANSI colours. The spec (§7.5) defines a polished gating surface that must convey
"stop and read" intent clearly — it's the moment the agent asks for trust.

## What to build

### Dimmed transcript context

The last 2–3 lines of the transcript above the permission gate render at reduced opacity
(`StyleDim`) to push visual focus toward the gate itself.

### Gate header

```
⚠ Agent Smith wants to edit a file       edit · path/to/foo.go · +3 −1
```

- `⚠` in `StyleRunning` (`ColorAmberBright`).
- `Agent Smith wants to …` in `ColorFgDefault`.
- Right-aligned summary: action · path · `+A −R` diff stats (`ColorCommand` for `+`,
  `ColorDiffRemovedText` for `−`).

### Diff block (bordered)

A Lipgloss box with `ColorBorder` border containing:

```
path/to/foo.go  @@ -10,4 +10,6 @@

  10 │   context unchanged
+ 11 │   added line one
+ 12 │   added line two
  13 │   context unchanged
- 14 │   removed line
  15 │   context unchanged
```

- Header row (`path @@ hunk @@`): `StyleNeutral`.
- Line number gutter: `BgInset` background, `StyleMuted` text.
- Added lines: background `#0c1f12`, gutter `#0e2415`, text `ColorDiffAddedText`
  (`#7dffa8`).
- Removed lines: background `#1f0e0e`, gutter `#241010`, text `ColorDiffRemovedText`
  (`#e08a8a`).
- Context lines: no background, gutter `BgInset`, text `#6f8a76`.

### Option list

```
❯  Yes, allow once                         ↵
   Yes, allow edits this session            a
   No, and tell Smith what to change       esc
```

- Selected option: background `BgModeBar`, `❯` in `ColorBrand`, border
  `ColorBorderSelect`.
- Non-selected: no background, no caret, text `StyleNeutral`.
- Key hint right-aligned on each row in `StyleDim`.
- Arrow keys / `j` `k` navigate; the bound key activates directly.

## Acceptance criteria

- `go test ./internal/tui/...` passes (existing permission tests must still pass).
- Permission gate renders with amber `⚠` header, bordered diff block using spec colours,
  and option list with `❯` selector on `BgModeBar`.
- Diff red (`#e08a8a`) appears only here — no other surface introduces non-phosphor
  hues.
- Keyboard navigation (↑↓, `a`, Esc) unchanged from AS-024 behaviour.
