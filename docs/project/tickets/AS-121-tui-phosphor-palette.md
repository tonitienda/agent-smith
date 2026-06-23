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
| `ColorDim` | `#4f6a57` | `241` |
| `ColorDimmest` | `#38503f` | `238` |
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
`StyleGoal`, `StyleCost`, `StyleError`, `StyleNeutral`, `StyleMuted`, `StyleDim`,
`StyleBanner`. (`StyleNeutral` = `ColorNeutral`, `StyleMuted` = `ColorMuted`; both are
relied on by AS-122/124/125/127/128/129/130/131, so they must exist here.)

`ColorDim` is the *brighter* dim and `ColorDimmest` the darkest — the green fade ramp
used by AS-126 (rain trail) and AS-131 (histogram) reads `ColorCommand → ColorNeutral
→ ColorMuted → ColorDim → ColorDimmest`, so the values must stay monotonically darker
in that order.

### Downstream-token rule (applies to AS-122/125/128/129/130)

Later tickets reference extra hexes (divider, mode-bar accents, diff backgrounds, etc.).
Those are **not** allowed as inline literals: each must be added as a named token to
`colors.go` by the ticket that needs it. AS-121's "no raw hex outside `colors.go`"
acceptance criterion holds for the whole `internal/tui/` tree, permanently.

### Tick strategy (shared contract for AS-122/123/124/125/126/130)

There is no single `tea.Tick` that all animations can reuse — caret blink (525 ms),
typewriter (40 ms), rain (~60 ms), spinner (the existing bubbles `spinner.TickMsg` in
`model.go`, ~110 ms), and pulses (~1.2 s) have incompatible periods. Pick one of:
fan out distinct `tea.Tick`s per cadence, **or** run one fast master tick (e.g. 40 ms)
and derive slower effects from a frame counter. Downstream tickets that say "reuse the
same tick" mean "do not add a redundant ticker for the same cadence" — they do not imply
one global period. State the chosen approach here when AS-121 lands.

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
