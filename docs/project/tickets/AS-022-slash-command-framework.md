---
id: AS-022
title: Slash-command framework + command palette
status: done
github_issue: 22
depends_on: [AS-021]
area: commands
priority: P0
source: PRD.md §7.6, §7.8, D6
---

# AS-022 · Slash-command framework

**Status: done**

## Description

The registry and UX that all built-in commands (`/cost`, `/context`, `/clean`, `/model`, …) plug into. Custom user-defined commands are fast-follow (D6) — but the framework should not preclude them.

- Command registry: name, summary, argument spec, handler; commands declare whether they render inline output or open a full-screen panel.
- Typing `/` in the input opens a filterable command palette (§7.8) with fuzzy matching and inline help.
- `/help` lists commands; unknown command → suggestion of nearest match.
- Argument parsing with quoted-string support (needed for `/clean "<topic>"` later).

## Acceptance criteria

- [x] Registering a command makes it appear in the palette and `/help` with zero TUI changes.
- [x] Palette filters as you type and completes on Tab/Enter.
- [x] Quoted arguments parse correctly.
- [x] Both inline and full-screen command render modes work (proven by two sample commands).

## Implementation notes

- `internal/command` — face-agnostic registry: `Command` (name, summary, arg
  spec, `Mode` inline/full-screen, handler), fuzzy `Match` for the palette,
  `Suggest` (Levenshtein) for "did you mean …?", quote-aware `Parse`, and a
  generic `HelpCommand` constructor any face can register.
- `internal/tui` — `/` opens a filterable palette (↑/↓ to select, Tab to
  complete, Enter to run); full-screen commands open a scrollable panel (esc/q
  to close); inline commands append a transcript segment. The TUI imports
  `internal/command` only (still no provider/tool imports).
- `cmd/smith` registers `/help` (full-screen) and `/version` (inline) as the two
  sample commands; the substantive commands land in AS-020/023/026/028.

## Dependencies

- AS-021 (TUI input + palette surface)
