---
id: AS-126
title: TUI Matrix rain — implement medium intensity as default, /serious disables
status: ready-to-implement
github_issue: null
depends_on: [AS-121, AS-053]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §5, §6, §7.1
---

# AS-126 — TUI Matrix rain (medium intensity, default on)

## Decision

Ship with **`medium` intensity as the default** rather than `subtle`. The full
phosphor-rain effect on splash/idle is the most memorable demo moment and is
immediately reversible via `/serious`. Three intensity levels remain the long-term plan
(subtle / medium / bold) but medium is the right out-of-the-box experience now.

## What AS-053 actually built

AS-053 established the **containment architecture**: a `workingLine` func injected into
the TUI, a `--no-splash` flag path, and structural comments reserving the
chrome-only surfaces. The visual effects themselves — digital rain columns, idle phrases,
`Mr. Anderson` naming, glitch-in on the logo — are **not yet rendered**. This ticket
builds them.

## What to build

### 1. Intensity enum + config

In `internal/tui/` (or a new `internal/personality/` package that the arch test allows
to touch only chrome):

```go
type Intensity int
const (
    IntensitySubtle Intensity = iota  // phosphor colours only, no rain
    IntensityMedium                   // + digital rain on idle/splash, idle phrases, Matrix names
    IntensityBold                     // + scanlines, CRT sweep, darker canvas, operator glyph
)
```

- Default: `IntensityMedium`.
- `/serious` sets `serious bool` to `true`; while true, render as if `IntensitySubtle`
  with plain names (`you`, `smith`, `sub-agents`). `/serious` again toggles it back.
- Config key `tui.intensity: subtle|medium|bold` (AS-031 layered config); runtime
  override via `--intensity` flag.

### 2. Digital rain (medium + bold, idle/splash only)

Render animated falling-character columns behind the splash screen and in the transcript
area when it is empty (no turns yet). Stop immediately when the user types or a turn
begins — never over content.

**Column model:**

```go
type rainColumn struct {
    x      int    // terminal column (0-based)
    y      float64 // current head row (sub-cell, updated on each tick)
    speed  float64 // rows/tick, randomised [0.3, 1.0]
    len    int    // trail length (chars that fade), randomised [4, 16]
    chars  []rune // random katakana/ascii sampled on spawn
}
```

- Character set: katakana `ァ–ン` (U+30A1–U+30F3) + digits `0–9` + a handful of ASCII
  punctuation. Sample a new random rune per column per tick for the head cell.
- Head cell: `ColorBrand` (`#00ff66`), bold.
- Trail: fade down the green ramp — `ColorCommand` → `ColorNeutral` → `ColorMuted` →
  `ColorDim` → `ColorDimmest` — one step per row behind the head.
- Tick rate: ~60 ms (match the typewriter tick; reuse the same `tea.Tick` if possible).
- Columns: one per 2 terminal columns, spawned with random offsets so the screen fills
  gradually rather than all at once.
- When the transcript area becomes non-empty or serious mode is toggled, drain the rain
  state and render nothing. The rain must not bleed into transcript rows.

**Render strategy:** the rain is a background layer composited _under_ the splash header
and invite text using Lipgloss `Place` or manual row-by-row rendering. The foreground
(logo, invite, input bar) always renders on top.

### 3. Idle phrases (medium + bold)

While the input is empty and no turn is running, cycle a rotating one-liner in `StyleMuted`
below the invite text. Swap every ~3 s:

```
following the white rabbit…
there is no spoon.
the matrix has you.
knock, knock, neo.
free your mind.
what is real?
```

Plain English; no themed phrases when serious is on.

### 4. `Mr. Anderson` naming in chrome (medium + bold)

In chrome-only surfaces (status line, mode bar, splash header context line):
- User display name: `Mr. Anderson` → replace `you` in the status line working-line.
- Sub-agents display label: `agents` → `the fleet`.
- Serious mode: revert to plain (`you`, `agents`).

These substitutions must live entirely in the `workingLine` / chrome rendering path and
must **not** touch transcript message bodies, tool names, or any substance surface.
The existing arch test (AS-053) should already guard this; extend it if needed.

### 5. Subtle logo glitch-in (medium only)

On startup, briefly render the logo with 1–2 random characters replaced by `░` or `▒`
for one frame (~80 ms) before settling to the normal `▞▞ AGENT SMITH`. Single one-shot
effect; do not repeat. Skip entirely when serious is on.

## Acceptance criteria

- `go test ./internal/tui/...` passes (add a test that `serious=true` produces no
  rain/phrases/Matrix names in any rendered output).
- Running `smith` without flags shows animated falling green characters on the splash
  screen and in the empty transcript area.
- Characters stop immediately on the first user message or tool turn.
- `/serious` in the command palette stops rain, phrases, and Matrix names instantly;
  `/serious` again restores them.
- `--intensity subtle` flag starts with no rain.
- Arch test confirms no import from the personality/chrome package into any substance
  renderer (transcript, tool output, diff, panel data).
