---
id: AS-116
title: Surface auto-escalation in /route and /cost + wire the first producer
status: ready-to-implement
github_issue: null
depends_on: [AS-110]
area: cost
priority: P2
source: PRD.md §7.15, §9; spun out of AS-110
---

# AS-116 · Surface auto-escalation in `/route` and `/cost` + wire the first producer

**Status: ready to implement** *(spun out of AS-110, which shipped the
`routing.Escalate` primitive and the per-session `/route` override path but
deferred escalation visibility and consumer wiring — there was no
structured-low-confidence/failed producer to wire yet.)*

## Description

AS-110 landed the auto-escalation mechanism (`routing.Escalate` + `NextTier`): a
tier-declared task can route its attempt through `Escalate`, which retries once on
the next stronger tier and returns an `Escalation{feature, from, to, reason}`
record for the caller to log. What is still missing is the part that needs a real
consumer:

1. **A first producer.** Wire `Escalate` into the first feature that actually
   returns a *structured low-confidence/failed result* — a candidate is the
   semantic `/clean` / `/tidy` analyzer or a model-using system sub-agent
   (e.g. the AS-109 insights-writer). The escalation must stay explicit and
   feature-owned — never an invisible retry for normal interactive chat turns
   (AS-042 clarified decision).

2. **Visibility in `/route` and `/cost`.** Persist the `Escalation` record (a log
   event, additive per D2) and render it: `/route` shows that an escalation
   occurred, which tiers it moved between, and why; `/cost` attributes the extra
   spend of the retry to the escalated turn. **§9 mitigation:** the escalation
   reason shown must be the structured reason the producer reported, grounded in
   the turn — never invented.

## Acceptance criteria

- [ ] At least one tier-declared feature routes its attempt through
      `routing.Escalate` and logs the returned `Escalation`.
- [ ] An escalation is visible in `/route` (the tiers moved between + reason) and
      its retry cost is attributed in `/cost`.
- [ ] Normal interactive chat turns never auto-escalate; only opted-in
      tier-declared tasks do.
- [ ] The escalation record is persisted additively (a new event kind/field), not
      a breaking change to existing events.

## Dependencies

- AS-110 (the `routing.Escalate` primitive + per-session override path)
