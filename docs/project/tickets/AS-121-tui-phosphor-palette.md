---
id: AS-121
title: TUI phosphor palette — centralize color tokens and apply to all surfaces
status: ready-to-implement
github_issue: null
depends_on: [AS-021, AS-053]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §2–3, internal/tui/CLAUDE.md invariant 1–2
---

# AS-121 — TUI phosphor palette

## Problem

`internal/tui/transcript.go` hardcodes ANSI 256-color indices (`"12"`, `"10"`, `"8"`,
etc.) for every role style. These don't match the specified phosphor green + amber
palette and produce a generic terminal look rather than the brand-consistent design.
`internal/tui/palette.go` is the *command palette*, not a color palette — there is no
`colors.go` or equivalent that centralises the token table.

## Goal

Replace every inline color reference in `internal/tui/` with named `lipgloss.Style`
/ `lipgloss.Color` values drawn from the canonical token table in
`docs/design/tui-visual-design.md §2`. Gate each token behind a Lipgloss profile so
truecolor terminals get the hex values and 256-color terminals get the nearest fallback.

## What to build

### 1. `internal/tui/colors.go` — the single source of truth

Declare one `lipgloss.Color` per token (or `lipgloss.AdaptiveColor` for fallbacks):

| Token | Truecolor | 256-color fallback |
|---|---|---|
| `ColorBrand` | `#00ff66` | `46` (bright green) |
| `ColorDone` | `#00cc52` | `40` |
| `ColorCommand` | `#7dffa8` | `120` |
| `ColorFgDefault` | `#c4e3cd` | `251` |
| `ColorNeutral` | `#9fb4a3` | `145` |
| `ColorMid` | `#7d9a84` | `108` |
| `ColorMuted` | `#5f7a66` | `65` |
| `ColorDim` | `#38503f` | `238` |
| `ColorDimmest` | `#4f6a57` | `241` |
| `ColorAmberBright` | `#ffb000` | `214` |
| `ColorAmberMuted` | `#caa24a` | `179` |
| `BgScreen` | `#0a0e0b` | `232` |
| `BgInset` | `#0c120e` | `232` |
| `BgModeBar` | `#103a22` | `22` |
| `BgStatusLine` | `#16201a` | `234` |
| `ColorBorder` | `#16241b` | `235` |
| `ColorBorderActive` | `#1c3322` | `22` |
| `ColorBorderSelect` | `#1f6b3f` | `28` |
| `ColorTree` | `#314a3a` | `240` |
| `ColorDiffAddedText` | `#7dffa8` | `120` |
| `ColorDiffRemovedText` | `#e08a8a` | `210` |
| `ColorDiffContextText` | `#6f8a76` | `102` |

Build named `lipgloss.Style` variables for every semantic role (§3 of the spec):
`StyleUser`, `StyleAssistant`, `StyleThinking`, `StyleToolName`, `StyleToolArgs`,
`StyleToolOutput`, `StyleSuccess`, `StyleRunning`, `StyleSlashCommand`, `StyleFilePath`,
`StyleGoal`, `StyleCost`, `StyleError`, `StyleDim`, `StyleBanner`.

### 2. Migrate all call sites

- `transcript.go` styles block (lines ~487–499): replace with the named role styles.
- `model.go` `modeBarStyle` / `statusBarStyle`: replace with token-backed styles.
- `permission.go`, `modal.go`, `meter.go`: audit and migrate any inline color literals.
- `picker.go` `paletteSelStyle` / `paletteItemStyle`: migrate to palette token styles.

No logic changes — colors only.

### 3. Command palette visual (§7.6)

While touching the palette styles, apply the spec layout:
- Search row: `❯ /█` in `ColorBorderSelect` border.
- Matched command name: `StyleSlashCommand` for `/serious`, `StyleNeutral` for others.
- Description: `StyleMuted`.
- Selected row background: `BgModeBar`.
- Footer hint line: `↑↓ move · ↵ run · tab complete · esc close` in `StyleDim`.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- Running `smith` in a truecolor terminal shows a green-on-dark-background theme: user
  labels in amber, assistant in bright green, tool output in dim green.
- Running in a 256-color terminal (e.g. `COLORTERM=` unset) shows reasonable colour
  approximations without broken escape sequences.
- No raw hex strings remain in `internal/tui/` outside `colors.go`.
