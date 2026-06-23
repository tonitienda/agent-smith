---
id: AS-130
title: TUI /agents orchestrator panel — tree view, state dots, pulsing animation
status: ready-to-implement
github_issue: null
depends_on: [AS-121, AS-044, AS-067]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §7.4
---

# AS-130 — /agents orchestrator panel

## Problem

No TUI panel exists for the `/agents` view. AS-044 built the sub-agent lifecycle
framework and AS-067 built the panel routing, but the orchestrator tree panel
was never implemented. The spec (§7.4) defines it as a key demo surface — showing
multiple agents working in parallel is a flagship differentiator.

## What to build

### Header row

```
◤ orchestrator · goal: fix the Esc-mid-turn spinner bug     3 dispatched · 12.4k tok · $0.008
```

- `◤` in `ColorBrand`.
- `orchestrator` in `StyleBanner`, goal text in `StyleGoal` (`ColorAmberMuted`).
- Aggregate stats right-aligned in `StyleNeutral`.

### Agent tree

Each sub-agent gets two rows:

```
├─ ● smith-scout     ◐ running    claude-haiku    2,341 tok    4.2 s
│  └ reading internal/tui/transcript.go …
├─ ✓ smith-analyst   ✓ done       claude-sonnet   8,102 tok    12.1 s
│  └ identified 3 candidate fix locations
└─ ○ smith-patcher   ○ queued     claude-sonnet   —            —
   └ waiting for scout + analyst
```

Row 1: tree glyph (`ColorTree`) · state dot · name · status · model · tokens · elapsed.
Row 2: `└` glyph (`ColorTree`) · one-line current/last action in `StyleMuted`.

**State dot colours and animation:**
| State | Glyph | Colour | Animation |
|---|---|---|---|
| running | `●` | `ColorAmberBright` | pulse 1.3 s (toggle between bright/mid) |
| done | `✓` | `ColorDone` | static |
| queued | `○` | `ColorDim` | static |
| partial/interrupted | `◐` | `ColorNeutral` | static |

Running agent's name row: `ColorFgDefault`. Done/queued: `StyleMuted`.

### Footer

```
▚▞ fleet running · 1 done · 1 active · 1 queued        saved ~8 s vs. serial
```

- `▚▞` operator glyph in `ColorBrand` (at medium/bold intensity only; omit when serious).
- Status summary in `StyleNeutral`.
- `saved ~Ns vs. serial` right-aligned in `StyleGoal`.

Flavor wording (the operator glyph, and any "fleet"/Matrix label for the sub-agents) is
chrome: gate it on `personality` intensity/serious and pull entity names from
`Personality.Name(personality.RoleSystemSubagents)` rather than hardcoding — same rule as
AS-126. The plain fallback (`sub-agents`, no glyph) must show under serious/`subtle`.

### Pulse tick

Reuse the existing model tick (same source as the rain tick from AS-126 / spinner from
AS-124). On each tick, toggle the running agent dot between `ColorAmberBright` and
`ColorAmberMuted`. No additional timer needed.

### Panel wiring

Register `/agents` as a panel command via the existing panel framework (AS-067).
The panel reads sub-agent state from whatever the runner exposes (AS-044 / AS-107);
if no agents are running, show an empty state: `no agents dispatched this session`
in `StyleDim`.

## Acceptance criteria

- `go test ./internal/tui/...` passes.
- `/agents` opens the panel and renders the header, tree, and footer.
- With no agents, shows the empty-state message.
- Running agent dot visibly pulses (testable by checking alternating renders from tick).
- Panel closes on `esc` / re-press of the hotkey (existing panel behaviour).
