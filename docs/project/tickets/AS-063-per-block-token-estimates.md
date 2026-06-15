---
id: AS-063
title: Per-block token estimates for window composition pricing
status: done
github_issue: 94
depends_on: [AS-020, AS-006]
area: cost
priority: P1
source: PRD.md §7.10, AS-020 follow-on
---

# AS-063 · Per-block token estimates

**Status: done**

Implemented in `internal/cost/estimate.go`: a stdlib-only chars-per-token
heuristic (`EstimateTokens`), a per-block estimator (`EstimateBlockTokens`) that
sums the model-facing payload of each body kind, and a window roll-up
(`EstimateContextTokens`) the `/context` view (AS-026) and meter (AS-025) call on
projection blocks. The method and its accuracy band are documented on the package
note; a reconciliation test sanity-checks the per-block sum against
provider-reported input usage.

## Description

Spun out of AS-020. AS-020 ships per-turn and per-session token + cost accounting
from provider-reported usage (`eventlog.KindUsage`), which reconciles exactly with
what providers bill. What it deliberately does **not** do is estimate the token
cost of an *individual* block in the projection — the data the always-visible
context meter (AS-025) and the `/context` composition view (AS-026) need to show
"this block is N tokens / $X of your window".

This ticket adds per-block token estimation:

- A tokenizer-based estimate (or provider-reported per-block count where a surface
  exposes one) for each projected block, so window composition can be priced.
- Feed the estimate into the projection layer / accounting so `/context` (AS-026)
  and the context meter (AS-025) can attribute window cost by block, topic, and
  segment.
- Keep it stdlib-only unless a tokenizer dependency is explicitly justified;
  a cheap heuristic estimate (e.g. chars/4) is an acceptable first form if an
  exact tokenizer would pull a heavy dependency — document the accuracy trade-off.

## Acceptance criteria

- [ ] Each projected block carries a token estimate available to `/context` and the
      context meter.
- [ ] The estimation method (exact tokenizer vs heuristic) and its accuracy are
      documented.
- [ ] Sum of per-block estimates for a turn's input is sanity-checked against the
      provider-reported input usage for that turn.

## Dependencies

- AS-020 (cost accounting + usage records)
- AS-006 (projection engine — where per-block metadata lives)
