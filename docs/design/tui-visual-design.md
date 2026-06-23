# Agent Smith TUI — Visual Design Specification

> Source of truth for the appearance and motion of Agent Smith's terminal face.
> Companion to `tui-reference.html` (interactive). Code lives in `internal/tui/`.
> Status: design reference · keep in sync with `internal/tui/` (Bubble Tea / Lipgloss).

This document is a **design reference**, not production code. Implement it with the
existing `internal/tui/` patterns (Bubble Tea models, Lipgloss styles). The HTML
companion is a prototype of intended look and behavior — recreate it in Go, don't port
the markup.

---

## 1. Medium & constraints

- **It's a terminal.** Type face, anti-aliasing, and cell size belong to the user's
  terminal — the app controls only **color**, **weight/dim/bold** (SGR), **layout**
  (Lipgloss boxes), and **glyph choice**. Pixel sizes in the HTML reference are
  illustrative of hierarchy, not literal.
- **Truecolor with graceful fallback.** Author every color as a Lipgloss style with a
  256-color fallback; detect the color profile, never assume 24-bit.
- **Character grid.** All structure is box-drawing + monospace alignment. Align columns
  with fixed cell widths, not ad-hoc spaces.
- **The reference font** is JetBrains Mono (approximates a modern dev terminal). Don't
  hard-code a font in the app.

---

## 2. Color tokens

Treat these as the canonical palette. Name them once (e.g. a `palette.go` style set);
reference the names everywhere else.

### Chrome / surfaces
| Token | Hex | Use |
|---|---|---|
| `bg.screen` | `#0a0e0b` | default terminal canvas |
| `bg.screen.bold` | `#060906` | canvas at `bold` intensity only |
| `bg.titlebar` | `#15191a` | window title bar (HTML chrome) |
| `bg.inset` | `#0c120e` | stat cards, search field, gutters |
| `bg.modebar` | `#103a22` | mode bar; also the selected-row fill |
| `bg.statusline` | `#16201a` | status line |
| `border` | `#16241b` | default rules / panel borders |
| `border.active` | `#1c3322` | border of a running tool card |
| `border.select` | `#1f6b3f` | selected row / focused field border |
| `divider.logo` | `#1d2c22` | rule under the splash logo (`#23402c` in bold) |
| `tree` | `#314a3a` | tree glyphs `├ └ │` |

### Text — green ramp (bright → dim)
| Token | Hex | Use |
|---|---|---|
| `green.bright` | `#00ff66` | brand, assistant name, 100%, key accent |
| `green.done` | `#00cc52` | success ✓, completed work |
| `green.command` | `#7dffa8` | slash commands, cost, "bright cyan" role |
| `fg.default` | `#c4e3cd` | primary body text |
| `green.neutral` | `#9fb4a3` | tool names, values, paths |
| `green.mid` | `#7d9a84` | secondary detail |
| `green.muted` | `#5f7a66` | paths, args, secondary chrome |
| `green.dim` | `#38503f` | placeholder / disabled |
| `green.dimmest` | `#4f6a57` | tool output body |

### Text — amber (the single warm accent)
| Token | Hex | Use |
|---|---|---|
| `amber.bright` | `#ffb000` | user name, warnings ⚠, running state |
| `amber.muted` | `#caa24a` | goal text, "working…" labels |

### Mode bar internals
`text #bdf0cf` · `accent #7dffa8` · `dim #4f8a64` / `#2f7a4c` / `#3f7a55`.

### Diff
| Part | bg | gutter | text |
|---|---|---|---|
| added `+` | `#0c1f12` | `#0e2415` | `#7dffa8` (comment `#4f8a64`) |
| removed `−` | `#1f0e0e` | `#241010` | `#e08a8a` |
| context | — | `#0c120e` (#`#3f5a47`) | `#6f8a76` |

> Diff red is the **only** non-phosphor hue in the system, justified because "removed"
> must read instantly. Keep it desaturated; never introduce other colors.

### /context window visualization (categorical, kept in-family)
system `#00ff66` · tools `#00cc52` · memory `#caa24a` · messages `#7dffa8` ·
files `#3f8a5a` · subagents `#2f6f4a` · free `#12211a` · auto-compact marker `#ffb000`.

### Traffic lights (HTML window chrome only)
`#ff5f56` / `#febc2e` / `#28c840`. Not part of the TUI itself.

---

## 3. Semantic role → color

The palette is applied by **meaning**, never by aesthetics. This mapping is stable:

| Role | Color |
|---|---|
| user / human turn | `amber.bright` `#ffb000` |
| assistant ("smith") | `green.bright` `#00ff66` |
| thinking / reasoning | `green.muted` `#5f7a66` |
| tool name | `green.neutral` `#9fb4a3` |
| tool args / params | `green.muted` `#5f7a66` |
| tool output body | `green.dimmest` `#4f6a57` |
| tool success ✓ | `green.done` `#00cc52` |
| tool running spinner | `amber.bright` `#ffb000` |
| slash command | `green.command` `#7dffa8` |
| file path | `green.neutral` `#9fb4a3` |
| goal | `amber.muted` `#caa24a` |
| cost / $ | `green.command` `#7dffa8` |

---

## 4. Typography roles & glyph vocabulary

Roles (expressed via weight/dim + size hierarchy, since the terminal owns the font):

- **Logo** — heaviest, `green.bright`: `▞▞ AGENT SMITH` (splash large; inline small).
- **Speaker label** — bold, role-colored, on its own line above the message.
- **Body** — `fg.default`, regular.
- **Secondary / chrome** — `green.muted`, often dimmed.
- **Micro-labels** — uppercase, wide letter-spacing, `green.muted` (panel headers).

Glyph vocabulary (use these, consistently):

| Glyph | Meaning |
|---|---|
| `▞▞` | brand mark / operator |
| `▚▞` | operator presence (chrome) |
| `✓` | tool / step success |
| `⣾⣽⣻⢿⣿⡿⣟⣯` | spinner cycle (braille) |
| `├─ └─ │` | orchestration / tree structure |
| `┃` | input prompt gutter |
| `█` | block cursor / meter fill |
| `░` | meter empty |
| `▇` | histogram bar |
| `● ◐ ○` | agent state: running / partial / queued |
| `⚠` | permission / warning (amber) |
| `❯` | selection caret / prompt |

---

## 5. The Matrix layer (AS-053) — the governing rule

The personality is a **theme on the chrome**, never on the substance. This is the
project's trust thesis: a coding agent must read as a precise instrument, with character
confined to the frame.

**Allowed surfaces:** splash / startup, idle and empty states, the status line, spinners
and idle phrases, the operator glyph, window/chrome accents.

**Forbidden surfaces (always plain):** transcripts, assistant/user message bodies, code,
diffs, file contents, tool output, and every data panel (`/context`, `/agents`,
`/insights`, permission diffs). No rain, no scanlines, no glitch, no themed names over
any of these.

### Intensity dial (additive)
- **`subtle`** — phosphor accent + soft glow on chrome, ASCII
  logo, blinking caret. Names stay plain: `you`, `sub-agents`. No rain.
- **`medium`** *(default — ships on, AS-126)* — adds: digital rain on idle/empty/splash
  **only**; subtle glitch-in on the logo; Matrix-flavored names in chrome
  (`Mr. Anderson`); rotating idle phrases ("following the white rabbit…", "there is no
  spoon…"). The rain is the signature demo moment and is instantly reversible via
  `/serious`; `"full"` is accepted as a legacy alias for `medium`.
- **`bold`** — adds: scanlines + a slow CRT sweep, stronger rain/glow, darker canvas
  `#060906`, operator glyph + presence line ("the system has you.").

### `/serious`
Instantly mutes the **entire** personality layer: plain names, no rain, no scanlines,
no idle phrases. Must be a single, obvious, reversible toggle.

---

## 6. Liveliness — motion as feedback

Every animation maps to real state. Timings from the reference:

| Effect | Spec |
|---|---|
| Streaming assistant text | typewriter, ~30–50 ms/char; trailing block cursor |
| Spinner | braille cycle, ~110 ms/frame; **amber** while running |
| Block cursor | blink, 1.05 s, hard steps (on/off) |
| Status "alive" pulse | opacity 0.55 → 1.0, 2.4 s ease-in-out |
| Subagent dot | pulse 1.3 s while running; solid when done; dim when queued |
| Idle phrase rotation | swap ~3 s (medium/bold only) |
| Digital rain | falling columns, idle/splash only (medium/bold) |
| CRT sweep | slow vertical pass, ~6.5 s (bold only) |

Rule: **no animation over static or completed content.** A finished tool card is still;
a running one ticks. Liveliness should let a user feel the agent working without ever
distracting from what it produced.

---

## 7. Screens

Continuity scenario across the reference: *fixing an "Esc mid-turn hangs the spinner" bug.*

### 7.1 Splash / startup
Logo `▞▞ AGENT SMITH` (`green.bright`), a rule, then a context line
`~/path · model · work mode` (`green.muted`). Empty state invites input
("Ask Agent Smith anything to begin.") with the command hints
(`type / for commands · Ctrl+G c context · /serious mute theme`) and a blinking caret on
the `┃` prompt. At `medium`/`bold`, rain falls behind and the greeting may go Matrix.

### 7.2 Work mode (primary)
Vertical stack, top → bottom:
1. **Transcript** (scrolls): inline splash header, then turns. Each turn = a bold
   role label line + body. Thinking shown dim. **Tool cards**: a status row
   (`✓`/spinner + tool name + args + elapsed) and an indented output block with a
   left rule (`border` / `border.active` when running), truncated with
   "… +N more lines — Ctrl+G t to expand". A streaming assistant reply ends in a live
   caret.
3. **Mode bar** (`bg.modebar`): the active mode and its phase track, e.g.
   `coding  scope · reproduce · [diagnose] · fix · verify`, current phase bracketed;
   `Ctrl+G m` right-aligned.
4. **Status line** (`bg.statusline`): `provider · model · session · goal` on the left;
   on the right a context meter (`█░` bar + `used/200k %`), cost, and the live spinner +
   "(Esc to cancel)".
5. **Input** (`┃` prompt): placeholder "Send a message (Enter to send, Alt+Enter for
   newline)…".

### 7.3 `/context` — the flagship
A token-budget dashboard.
- **Segmented bar** across the 200k window, one segment per category (colors in §2),
  with an **amber vertical marker** at the auto-compact threshold (180k / 90%) labeled
  `auto-compact 180k`. Scale labels `0 … 86,431 / 200,000 used · 43% … 200k`.
- **Legend grid** (2 col): swatch · category · tokens · %. Categories: System prompt,
  Tool definitions, Project memory (CLAUDE.md), Message history, File contents,
  Subagent results, Free, plus a `CLAUDE.md loaded · N rules ✓` row.
- A **tip** line suggesting `/compact` to reclaim tokens.
- **Stats rail** (inset card): window / used / free, cache read / write / hit-rate,
  and session cost (large, `green.command`).

### 7.4 `/agents` — orchestration
An orchestra view.
- Header: `◤ orchestrator · goal: … ` with aggregate (`N dispatched · tokens · $`).
- A **tree** (`├─ └─ │`) of subagents, each row: state dot (`● ◐ ○`) · name · status
  (`✓ done` / `◐ running` / `○ queued`) · model · tokens · elapsed; a sub-line
  (`└ …`) describes current/last action. The **running** agent's dot pulses (amber);
  the running row's name is `fg.default`, others muted.
- Footer: `▚▞ fleet running · 1 done · 1 active · 1 queued` and a
  `saved ~Ns vs. serial` figure.

### 7.5 Permission + diff
A gate before any write/shell.
- Dimmed transcript context above (the agent's stated intent).
- `⚠ Agent Smith wants to edit a file` (amber) + `edit · path · +A −R`.
- A **diff** in a bordered box: header `path  @@ hunk @@`, line-number gutter, colored
  `+`/`−` rows (§2 diff colors), context rows plain.
- **Option list**, first selected (`❯`, on `bg.modebar`/`border.select`):
  `Yes, allow once ↵` · `Yes, allow edits this session  a` ·
  `No, and tell Smith what to change  esc`.

### 7.6 Command palette
Opens on `/`.
- Search field (`❯ / █`, `border.select`) with a `N commands` count.
- Fuzzy-filtered list; selected row on `bg.modebar` with `❯`. Each row: command
  (`green.command`/`neutral`) + description (`green.muted`). `/serious` shown in amber.
- Footer key hints: `↑↓ move · ↵ run · tab complete · esc close`.
- Command set seen in the reference: `/context /agents /compact /diff /mode /model
  /insights /serious /config /cost` (+ standard `/help /quit`).

### 7.7 `/insights` — session retrospective
- **Stat grid** (4 cards): turns · tokens · cost · wall-time (cost green, time amber).
- **"what happened"** timeline: `✓`/`◐`/`○` step rows
  (scoped → reproduce → diagnose → fix → verify) with one-line descriptions.
- **Tool-call histogram**: `name  ▇▇▇▇  count`, bars colored down the green ramp,
  the edit row in amber.

---

## 8. Lipgloss implementation notes

- Centralize the palette as named `lipgloss.Style` / `lipgloss.Color` values; build
  every component from the named roles in §3 — no inline hex at call sites.
- Gate the personality behind one intensity enum (`subtle|medium|bold`) and a master
  `serious` bool. The render path for substance must be identical regardless of theme —
  the theme only adds chrome layers.
- Keep animation tick rates in §6 in named constants; drive them from real model state
  (streaming, tool running, agent active), not timers that run on idle content.
- Provide 256-color fallbacks for every truecolor value.

---

## 9. Files

- `internal/tui/` — the implementation (palette, transcript, tool cards, meter, mode
  bar, modal, model).
- `docs/design/tui-reference.html` — interactive reference (open in a browser; shows
  the intensity dial, a live Work-mode screen, and all panels).
- `internal/tui/CLAUDE.md` — the always-on contract (the invariants subset).
- `docs/UX.md`, `docs/design/adr-0001-tui-framework.md` — product/UX background.
