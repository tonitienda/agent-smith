---
id: AS-086
title: Conservative budget enforcement — pre-turn estimate + unpriced-turn handling
status: done
github_issue: 148
depends_on: [AS-041, AS-063]
area: cost
priority: P2
source: AS-041 review (Copilot)
---

# AS-086 · Conservative budget enforcement

**Status: ready to implement**

## Description

Spun out of AS-041. The budget guard (AS-041) enforces at turn boundaries against
*recorded, priced* spend (`cost.Summarize(...).TotalUSD`). That leaves two honest
gaps, documented in AS-041 rather than hidden:

1. **Single-turn overshoot.** A turn's cost is known only after it completes, so a
   turn can carry the session total slightly past the ceiling before the next
   boundary halts the run. Strict "halt *before* exceeding" needs a pre-turn
   reservation — a conservative estimate of the next turn's cost (request-size
   input tokens via AS-063 per-block estimates + the model's `max_output` at the
   output rate) checked against remaining budget before the request is issued.
2. **Unpriced-model bypass.** A turn whose model the pricing table cannot price
   contributes `$0` to `TotalUSD` (`summary.AllPriced == false`), so an
   unknown/unpriced model is effectively unmetered and can run past the ceiling.
   Enforcement should be conservative when costs are unknown — e.g. surface a
   one-time warning that the budget cannot be enforced for an unpriced model, and
   optionally (config-flagged) halt rather than spend blind.

## Acceptance criteria

- [x] With a pre-turn estimate, a session cannot start a turn whose worst-case
      cost would exceed the remaining budget (test with mock pricing + a known
      `max_output`). — `loop.WithBudgetReservation` reserves the worst-case next
      turn and halts before issuing it (`TestBudgetReservationHaltsBeforeOvershoot`).
- [x] A session on an unpriced model with a budget set surfaces a clear "budget
      not enforceable (unpriced model)" notice rather than silently spending; the
      halt-vs-warn behavior is config-flagged. — `UIBudgetUnpriced` (once per run)
      + `budget.halt_unpriced` config (`TestBudgetUnpricedWarnsOnce`,
      `TestBudgetUnpricedHaltsWhenConfigured`).
- [x] The estimate reuses AS-063 per-block token estimates rather than a second
      tokenizer path. — `cost.EstimateTurnCostUSD` builds on
      `cost.EstimateContextTokens` + the new `max_output_tokens` pricing field.
- [x] AS-041's boundary-based path remains the fallback when no estimate is
      available, so behavior degrades gracefully. — a nil reservation leaves only
      the boundary check (`TestBudgetReservationFallsBackToBoundary`).

## Implementation notes

- `cost.Rate` gains an additive `max_output_tokens` field (populated for the
  bundled models in `pricing.json`); `cost.EstimateTurnCostUSD(ctx, model, table)`
  prices request-size input + max output, returning `ok=false` for an unpriced or
  max-output-less model.
- `loop.WithBudgetReservation(reserve, haltUnpriced)` adds the pre-turn check on
  top of AS-041's `WithBudget`; the loop projects the context once per iteration
  and reuses it for both the reservation and the request.
- `budget.halt_unpriced` (default off) is read in `cmd/smith/chat.go` and wired
  through the controller; the TUI renders the `UIBudgetUnpriced` notice.

## Dependencies

- AS-041 (budget guard + `/budget`), AS-063 (per-block token estimates)
