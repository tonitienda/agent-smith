---
id: AS-020
title: Token & cost accounting engine + /cost command
status: done
github_issue: 20
depends_on: [AS-009, AS-010, AS-022]
area: cost
priority: P0
source: PRD.md §7.10, §7.16, D5, D6
---

# AS-020 · Token & cost accounting + /cost

**Status: done**

## Implementation notes

- The loop captures each provider turn's `EventUsage` events (input/cache at the
  turn start, output at the end), accumulates them field-wise, and records one
  `eventlog.KindUsage` control event per turn carrying `Tokens`, `UsageMeta`, the
  serving model (`Provider`), and the stop reason. Usage is **derivable from the
  log** — no side table — so it survives save/resume and reconciles exactly with
  the provider-reported counts. Like `KindExclusion`, the usage kind is a
  harness control event the projection engine never renders into model context.
- `internal/cost` is the data layer: `Summarize` prices the log's usage events
  into per-turn and per-session records (input/output/cache-read/cache-write
  split, total, and cache savings in tokens + $); `Render` formats the `/cost`
  report. Pricing ships as embedded data (`data/pricing.json`), overridable per
  session by a file named in `$SMITH_PRICING` that layers over the embedded
  defaults model-by-model. An unknown model degrades gracefully: tokens are
  exact, the dollar figure is shown as `—`, and the session total is flagged a
  lower bound.
- `/cost` is registered in the chat face (`cmd/smith/chat.go`), closing over the
  session log and pricing table, and renders full-screen.

## Description

Live per-turn and per-session token + $ accounting, broken down by input / output / cache (§7.10). This is the data layer for the context meter, `/context`, and the D5 guardrail benchmarks — accuracy matters more than presentation here.

- Pricing table: per-model input/output/cache-read/cache-write rates, shipped as data (embedded file, user-overridable in config) so price changes don't need releases. Unknown models → tokens shown, $ marked unknown.
- Per-turn records built from provider usage events (AS-009/010/011), attributed to the session and stored derivably from the log.
- Per-block token estimates (tokenizer-based or provider-reported where available) so window composition can be priced — feeds AS-026 `/context`.
- `/cost` command: session totals, per-turn table, input/output/cache split, cache savings.

## Acceptance criteria

- [x] Session totals reconcile exactly with the sum of provider-reported usage.
- [x] Cache savings displayed in tokens and $.
- [x] Pricing table is overridable without recompiling; unknown model degrades gracefully.
- [x] `/cost` renders in the TUI with per-turn breakdown.

## Dependencies

- AS-009, AS-010 (usage events; AS-011 enriches cache fields)
- AS-022 (slash-command framework for `/cost`)
