---
id: AS-022
title: Slash-command framework + command palette
status: ready-to-implement
github_issue: null
depends_on: [AS-021]
area: commands
priority: P0
source: PRD.md §7.6, §7.8, D6
---

# AS-022 · Slash-command framework

**Status: ready to implement**

## Description

The registry and UX that all built-in commands (`/cost`, `/context`, `/clean`, `/model`, …) plug into. Custom user-defined commands are fast-follow (D6) — but the framework should not preclude them.

- Command registry: name, summary, argument spec, handler; commands declare whether they render inline output or open a full-screen panel.
- Typing `/` in the input opens a filterable command palette (§7.8) with fuzzy matching and inline help.
- `/help` lists commands; unknown command → suggestion of nearest match.
- Argument parsing with quoted-string support (needed for `/clean "<topic>"` later).

## Acceptance criteria

- [ ] Registering a command makes it appear in the palette and `/help` with zero TUI changes.
- [ ] Palette filters as you type and completes on Tab/Enter.
- [ ] Quoted arguments parse correctly.
- [ ] Both inline and full-screen command render modes work (proven by two sample commands).

## Dependencies

- AS-021 (TUI input + palette surface)
