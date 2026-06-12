---
id: AS-020
title: Token & cost accounting engine + /cost command
status: ready-to-implement
github_issue: null
depends_on: [AS-009, AS-010, AS-022]
area: cost
priority: P0
source: PRD.md §7.10, §7.16, D5, D6
---

# AS-020 · Token & cost accounting + /cost

**Status: ready to implement**

## Description

Live per-turn and per-session token + $ accounting, broken down by input / output / cache (§7.10). This is the data layer for the context meter, `/context`, and the D5 guardrail benchmarks — accuracy matters more than presentation here.

- Pricing table: per-model input/output/cache-read/cache-write rates, shipped as data (embedded file, user-overridable in config) so price changes don't need releases. Unknown models → tokens shown, $ marked unknown.
- Per-turn records built from provider usage events (AS-009/010/011), attributed to the session and stored derivably from the log.
- Per-block token estimates (tokenizer-based or provider-reported where available) so window composition can be priced — feeds AS-026 `/context`.
- `/cost` command: session totals, per-turn table, input/output/cache split, cache savings.

## Acceptance criteria

- [ ] Session totals reconcile exactly with the sum of provider-reported usage.
- [ ] Cache savings displayed in tokens and $.
- [ ] Pricing table is overridable without recompiling; unknown model degrades gracefully.
- [ ] `/cost` renders in the TUI with per-turn breakdown.

## Dependencies

- AS-009, AS-010 (usage events; AS-011 enriches cache fields)
- AS-022 (slash-command framework for `/cost`)
