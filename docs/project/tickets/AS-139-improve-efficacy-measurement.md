---
id: AS-139
title: /improve proposal efficacy measurement (before/after friction delta)
status: done
github_issue: null
depends_on: [AS-058, AS-057, AS-136]
area: insights-wedge
priority: P3
source: spun out of AS-138
---

# AS-139 · /improve proposal efficacy measurement

**Status: done** *(spun out of AS-138)*

## Implementation notes

- The **application marker** is the existing `skillrollup` tombstone, now enriched:
  `Store.ResolveApplied(kind, summary, target, diff)` records the applied
  proposal's target + edit (the proposal `Key`) alongside the finding signature
  and timestamp it already carried. `Resolve` delegates to it with empty
  target/diff, so the extra fields are additive (D2) and idempotent. Both
  `/improve apply` and `/skills apply` now call `ResolveApplied`.
- The **before/after delta** is computed deterministically in `skillrollup`
  (`efficacyFrom` → `Report.Efficacy`): the earliest tombstone for a signature is
  the application moment; findings of that signature are split into those recorded
  at/before it (`Before`) and after it (`After`, the post-apply recurrences, with
  distinct `SessionsAfter`). `After == 0` is the proxy that the edit worked. Only
  applied signatures appear; no model call.
- **Surfaced** in the `stats` report (`Report.Improvements` + an "Applied
  improvements" render section), shared by the `/stats` TUI panel and the headless
  `smith stats` verb. Default `/stats` scope is the current project, satisfying the
  "same project" requirement; the `all` scope extends it portfolio-wide.

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

- [x] Applying an `/improve` proposal records a durable, additive application
      marker (Key + finding signature + timestamp).
- [x] A before/after friction delta is computed per applied proposal for the same
      project, using the AS-057/AS-136 metrics (the simplest proxy: did the
      targeted finding stop recurring in sessions after the application?).
- [x] The delta is surfaced in `/stats` or `/insights` and its headless face.
- [x] The marker and any persisted fields are additive — an older reader ignores
      them.

## Dependencies

- AS-058 (the `/improve` queue + `internal/improve`, where application happens),
  AS-057 (cross-session friction metrics), AS-136 (persisted cross-session stats
  index).
