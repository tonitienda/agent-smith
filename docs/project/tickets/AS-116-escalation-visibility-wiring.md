---
id: AS-116
title: Surface auto-escalation in /route and /cost + wire the first producer
status: done
github_issue: 381
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

- [x] At least one tier-declared feature routes its attempt through
      `routing.Escalate` and logs the returned `Escalation`. *(`/compact --apply`
      routes its cheap-tier summarization through `routing.Escalate`; an empty
      summary — a structured low-confidence result — escalates once to the next
      stronger tier and logs the `Escalation`.)*
- [x] An escalation is visible in `/route` (the tiers moved between + reason) and
      its retry cost is attributed in `/cost`. *(`/route` renders an "Escalations"
      section from the logged events; each attempt records its own
      `compact.Producer` usage event, so the failed cheap call and the successful
      retry are both itemized in `/cost`.)*
- [x] Normal interactive chat turns never auto-escalate; only opted-in
      tier-declared tasks do. *(Escalation is wired only into the explicit,
      user-invoked `/compact --apply`; the interactive loop and auto-compact are
      untouched.)*
- [x] The escalation record is persisted additively (a new event kind/field), not
      a breaking change to existing events. *(New `eventlog.KindEscalation` control
      event; the payload rides on the schema's additive `Block.Ext` escape hatch,
      so the frozen content-block union (AS-003) is untouched — D2.)*

## Resolution

Shipped: the first escalation producer (`/compact --apply`) and the visibility
path. `/compact` resolves its summarization tier through the router (AS-042) and
wraps the attempt in `routing.Escalate`; when the summarizer returns an empty
summary it escalates once (cheap → standard by default) and retries. A new
additive `eventlog.KindEscalation` control event (payload on `Block.Ext`, never
rendered into model context) records the feature, the tiers moved between, and the
producer's structured reason (§9: grounded, never invented). `/route` reads those
events and renders an "Escalations" section; because each attempt logs its own
`compact.Producer` usage event, `/cost` already attributes the retry's extra
spend. A transport/provider error is *not* treated as a low-confidence result, so
it never escalates. Auto-escalation stays explicit and feature-owned — only the
opted-in `/compact` task escalates, never a normal chat turn or auto-compact
(AS-042 clarified decision).

## Dependencies

- AS-110 (the `routing.Escalate` primitive + per-session override path)
