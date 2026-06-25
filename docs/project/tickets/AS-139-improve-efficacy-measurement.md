---
id: AS-139
title: /improve proposal efficacy measurement (before/after friction delta)
status: ready-to-implement
github_issue: null
depends_on: [AS-058, AS-057, AS-136]
area: insights-wedge
priority: P3
source: spun out of AS-138
---

# AS-139 · /improve proposal efficacy measurement

**Status: ready to implement** *(spun out of AS-138)*

## Description

AS-058 AC 4 — "applying proposals reduces the measured friction in subsequent
sessions" — was deferred from AS-138 because it is a longitudinal concern, not a
single-session signal like the confidence threshold AS-138 shipped. It needs:

1. **Record the application event durably.** When an `/improve` proposal is
   applied, persist a marker tying the applied proposal (its `Key` — target +
   normalized edit — and the underlying finding signature) to the moment it was
   applied, so later sessions can be attributed as "after" the change. The
   `internal/improve` Ledger or the `skillrollup` log is the natural home; keep it
   additive (D2).

2. **Link to subsequent-session friction.** Use the AS-057 cross-session friction
   metrics (and the AS-136 cross-session stats index) to compute a before/after
   delta for the same project: did the friction the proposal targeted drop in the
   sessions recorded after it was applied? The rediscovered-fact rollup already
   groups by signature, so "did this fact stop recurring after the edit landed?"
   is the first, deterministic proxy for efficacy — no model call needed.

3. **Surface the delta.** Show the before/after in `/stats` or `/insights` (and
   the headless equivalents) so a user can see whether their applied improvements
   are paying off.

Keep it deterministic where possible.

## Acceptance criteria

- [ ] Applying an `/improve` proposal records a durable, additive application
      marker (Key + finding signature + timestamp).
- [ ] A before/after friction delta is computed per applied proposal for the same
      project, using the AS-057/AS-136 metrics (the simplest proxy: did the
      targeted finding stop recurring in sessions after the application?).
- [ ] The delta is surfaced in `/stats` or `/insights` and its headless face.
- [ ] The marker and any persisted fields are additive — an older reader ignores
      them.

## Dependencies

- AS-058 (the `/improve` queue + `internal/improve`, where application happens),
  AS-057 (cross-session friction metrics), AS-136 (persisted cross-session stats
  index).
