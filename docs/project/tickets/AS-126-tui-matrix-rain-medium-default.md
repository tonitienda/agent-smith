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

## Reconcile with the existing `internal/personality` package

AS-053 already shipped `internal/personality/personality.go` with a *string* intensity
of `full | subtle` (`subtle` = status/loading lines only, no renaming), the
`/serious` kill switch (`ToggleSerious`), `RoleSystemSubagents → "Agents"`, and its own
`matrixStatusLines`. This ticket must **extend that package, not fork it** — do not
declare a parallel `Intensity` type in `internal/tui/`. Concretely:

- Widen `personality.Settings.Intensity` to the three-value set `subtle | medium | bold`
  (additively — keep `full` as an accepted alias for `medium` so existing config still
  parses, PRD D2). Default resolves to `medium`.
- Map the existing `subtle bool` field onto the new enum; `medium`/`bold` are the
  flavor-on intensities. Renaming (Matrix names) stays gated to `medium`/`bold`, matching
  today's `!subtle` check.
- The rain, idle phrases, glitch-in, scanlines etc. are *render-side* concerns: the
  personality package answers "which intensity / is serious" and owns the flavor strings;
  the rain animation itself lives in the chrome render path (`internal/tui`), which is
  already allowed to import `personality`.

### 1. Intensity enum + config

```go
type Intensity int
const (
    IntensitySubtle Intensity = iota  // phosphor colours only, no rain
    IntensityMedium                   // + digital rain on idle/splash, idle phrases, Matrix names
    IntensityBold                     // + scanlines, CRT sweep, darker canvas, operator glyph
)
```

- Declare this enum in `internal/personality` and resolve `Settings.Intensity` to it.
- Default: `IntensityMedium`.
- `/serious` calls the existing `ToggleSerious`; while serious, render as if
  `IntensitySubtle` with plain names (`you`, `smith`, `sub-agents`).
- Config key `personality.intensity: subtle|medium|bold` (AS-031 layered config; this is
  the existing `personality` section, not a new `tui` one); runtime override via
  `--intensity` flag.

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

- Character set: **half-width** katakana `ｱ–ﾝ` (U+FF71–U+FF9D) + digits `0–9` + a
  handful of ASCII punctuation. Half-width katakana are single-cell in monospaced
  terminal fonts; full-width (`ァ–ン` U+30A1–U+30F3) are double-cell and would cause
  column jitter as characters change — never use full-width in the rain columns.
  Sample a new random rune per column per tick for the head cell.
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
below the invite text. Swap every ~3 s. **Source these from the existing
`matrixStatusLines` in `personality.go`** (add any of the phrases below that are missing
rather than maintaining a second list):

```
following the white rabbit…
there is no spoon.
the matrix has you.
knock, knock, neo.
free your mind.
what is real?
```

Plain English; no themed phrases when serious is on. The rotation is already implemented
by `Personality.StatusLine()` (clock-bucketed, stateless) — reuse it; don't reinvent.

### 4. `Mr. Anderson` naming in chrome (medium + bold)

The name map already exists in `personality.go` (`matrixNames`): `RoleUser → "Mr.
Anderson"`, `RoleSystemSubagents → "Agents"`, etc. **Use `Personality.Name(role)` for
every chrome display-name** — do not hardcode substitutions in the render path. If a
label should change (e.g. sub-agents reading as "the fleet" rather than "Agents"), edit
`matrixNames`/`plainNames` in `personality.go` so the one map stays the source of truth;
this ticket's default keeps the shipped `"Agents"` unless the design explicitly changes
it.

Names render only at `medium`/`bold`; serious mode and `subtle` fall back to `plainNames`
(`you`, `sub-agents`) — already handled by `Name()`. The arch test
(`internal/personality/no_business_imports_test.go`) already guards that no substance
path imports this package; extend it if you add new chrome callers.

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
