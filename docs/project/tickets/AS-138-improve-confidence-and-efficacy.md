---
id: AS-138
title: /improve high-confidence single-fact threshold
status: done
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

The second deferred piece — **efficacy measurement** (AS-058 AC 4) — was a
substantially larger, longitudinal concern (linking applied proposals to later
friction metrics) and was spun out to **AS-139** so this ticket stays a focused,
deterministic confidence-threshold change.

Keep additive (D2) and deterministic where possible.

## Acceptance criteria

- [x] A single rediscovered fact above a confidence threshold yields a proposal
      without waiting for a second session; low-confidence single facts still wait
      for recurrence.
- [x] The confidence signal flows through the persisted rollup record additively
      (an older reader ignores it).
- [ ] ~~Applied proposals linked to subsequent-session friction metrics~~ — spun
      out to **AS-139** (efficacy measurement).

## Implementation notes

- The confidence signal is the rediscovered-fact detector's **count of failed
  prior attempts** that justify a fact (`len(candidate.Failed)`): the more the
  agent flailed before settling, the more durable the fact. It threads additively
  through `subagent.Finding.Confidence` → `skillrollup.Record.confidence` (json,
  unknown-field-tolerant) → `skillrollup.Group.Confidence` (max across the group).
- `improve.HighConfidence = 3`: a single-session finding grounded by ≥3 failed
  attempts is proposed immediately; weaker single-session facts still wait for
  `MinSessions` recurrence. The proposal renders "high-confidence single fact" so
  the grounding for a one-session promotion is legible.

## Dependencies

- AS-058 (the `/improve` queue + `internal/improve`), AS-048 (fact confidence),
  AS-057 (cross-session friction metrics)
