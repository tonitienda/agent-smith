---
id: AS-137
title: /insights on-demand model retro when the writer is disabled (spun out of AS-109)
status: done
github_issue: 423
depends_on: [AS-109]
area: insights-wedge
priority: P2
source: PRD.md §7.14; spun out of AS-109
---

# AS-137 · /insights on-demand model retro

**Status: ready to implement** *(spun out of AS-109)*

## Description

AS-109 landed the insights model-assisted layer as a session-end pass in the
insights-writer (cheap tier, budget-capped, grounded), plus deterministic goal
anchoring that renders on demand. What it deliberately deferred is the **on-demand
path**: when the insights-writer's model layer is *disabled*, `/insights` (and
`smith insights`) should offer to run the model retro **once, now**, on the current
session — the measured dashboard and goal anchoring already render for free
regardless.

The substrate exists: `internal/insightsmodel.Proposer.Propose(ctx, Report)`
returns grounded, model-authored suggestions and the dollars spent, and the
panel/controller already render an `insights.Report`. This ticket wires the
controller `/insights` seam to call the proposer on demand (behind a confirm,
since it spends), merge the grounded suggestions into the rendered report, and
charge the call against the session budget — without enabling the always-on
session-end layer.

## Acceptance criteria

- [ ] On a session whose insights-writer model layer is disabled, `/insights`
      offers to run the model retro once on demand; declining leaves the measured
      dashboard (and goal anchoring) rendering exactly as before.
- [ ] Accepting runs the cheap-tier proposer once, merges only the grounded
      (evidence-citing) suggestions, labels them model-authored, and charges the
      spend against the session budget (skipped if the budget has no room).
- [ ] Headless parity: `smith insights` exposes the same one-shot via a flag
      (e.g. `--describe`/`--model`), off by default so the base command stays
      deterministic and free.

## Dependencies

- AS-109 (the `insights.Proposer` seam, `internal/insightsmodel`, goal anchoring).
