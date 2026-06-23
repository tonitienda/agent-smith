---
id: AS-122
title: TUI splash / startup screen — logo, context line, invite text, blinking caret
status: ready-to-implement
github_issue: 387
depends_on: [AS-121, AS-126]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.1
---

# AS-122 — TUI splash / startup screen

## Problem

The current splash header (`▞▞ AGENT SMITH` + metadata line in `transcript.go:306`)
exists but falls short of the spec. It:
- Uses `bannerStyle` (ANSI 10) instead of `ColorBrand` / `StyleBanner` from AS-121.
- Shows no divider rule below the logo.
- Renders no invite copy or command hints in the empty state.
- Has no blinking block caret on the input prompt glyph (`┃`).

## Goal

Make the startup screen match `docs/design/tui-visual-design.md §7.1` exactly, so the
first thing a user sees in a demo is a polished, brand-consistent splash.

## What to build

### Splash header (already partially rendered in `transcript.go:renderStartupHeader`)

```
▞▞ AGENT SMITH
─────────────────────────────────────────
~/project · claude-sonnet-4 · code
```

- Logo `▞▞ AGENT SMITH` in `StyleBanner` (`ColorBrand`, bold).
- Horizontal rule below the logo: full terminal width in `ColorDividerLogo`
  (`#1d2c22`; add this token to `colors.go`).
- Context line: `path · model · work mode` in `StyleMuted`.

### Empty-state invite (shown only when transcript has no user/assistant turns)

```
Ask Agent Smith anything to begin.

  type / for commands · Ctrl+G c context · /serious mute theme
```

- Invite headline in `StyleNeutral`.
- Hint row in `StyleDim`.
- Rendered inline in the transcript area above the input bar (not a separate overlay).

### Blinking caret on `┃` prompt glyph

- The input gutter character (`┃`) should blink at 1.05 s (on/off, hard steps).
- Drive it from a `tea.Tick` at 525 ms that toggles a `caretVisible bool` in the model.
- Stop blinking as soon as the first character is typed in the input area; resume when
  it clears back to empty.
- The caret itself is just the `┃` rendered alternately in `ColorBrand` vs `ColorDim`.
  No cursor-escape tricks.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- `smith` starts and shows the logo, rule, context line, invite copy, and command hints
  before any message is sent.
- The `┃` glyph blinks when the input is empty and stops when the user types.
- `--no-splash` still suppresses everything above the input bar (existing behaviour).
- At `medium`/`bold` Matrix intensity (default) the rain from AS-126 renders behind
  the invite copy and the idle phrase rotation replaces the static hint after 3 s.
