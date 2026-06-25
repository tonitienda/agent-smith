---
id: AS-138
title: /improve high-confidence single-fact threshold + efficacy measurement
status: ready-to-implement
github_issue: null
depends_on: [AS-058, AS-048, AS-057]
area: insights-wedge
priority: P3
source: spun out of AS-058
---

# AS-138 · /improve high-confidence single-fact threshold + efficacy measurement

**Status: ready to implement** *(spun out of AS-058)*

## Description

AS-058 shipped the self-improving config queue (`/improve`, `smith improve`,
`internal/improve`) with the recurrence threshold only: a finding is proposed
once it recurs across `improve.MinSessions` (=2) distinct sessions. Two pieces of
the AS-058 clarified scope were deferred because the upstream data does not exist
yet:

1. **High-confidence single-fact threshold.** AS-058's threshold was "≥2 sessions
   *or* one high-confidence AS-048 durable fact." The cross-session findings
   `Record` (AS-050 `skillrollup`) carries no confidence/precision score, so a
   single high-confidence durable fact cannot be distinguished from any other
   one-session finding. This ticket threads a confidence signal from the
   rediscovered-fact detector (AS-048/AS-106) through the rollup record so a
   single high-confidence fact can be promoted to a proposal immediately.

2. **Efficacy measurement (AS-058 AC 4).** "Applying proposals reduces the
   measured friction in subsequent sessions" needs longitudinal data: tie an
   applied `/improve` proposal to the later AS-030/AS-057 friction metrics for the
   same project and show whether friction dropped. Surface it in `/stats` or
   `/insights`.

Keep both additive (D2) and deterministic where possible.

## Acceptance criteria

- [ ] A single rediscovered fact above a confidence threshold yields a proposal
      without waiting for a second session; low-confidence single facts still wait
      for recurrence.
- [ ] The confidence signal flows through the persisted rollup record additively
      (an older reader ignores it).
- [ ] Applied proposals are linked to subsequent-session friction metrics and a
      before/after delta is shown in `/stats` or `/insights`.

## Dependencies

- AS-058 (the `/improve` queue + `internal/improve`), AS-048 (fact confidence),
  AS-057 (cross-session friction metrics)
