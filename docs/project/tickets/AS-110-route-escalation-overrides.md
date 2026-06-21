---
id: AS-110
title: Model routing escalation + per-session /route overrides
status: ready-to-implement
github_issue: null
depends_on: [AS-042]
area: cost
priority: P2
source: PRD.md §7.15; spun out of AS-042
---

# AS-110 · Routing escalation + per-session `/route` overrides

**Status: ready to implement** *(spun out of AS-042, which shipped the tier abstraction, config policy, and the read-only `/route` inspector)*

## Description

AS-042 landed the routing substrate: a `cheap | standard | strong` tier policy
(`internal/routing`), config-driven tier→model mappings with per-feature
overrides, `/compact` resolving its model through the router, and a read-only
`/route` inspector. Two pieces were explicitly deferred because no consumer
needed them yet:

1. **Auto-escalation (PRD §7.15 "auto-escalate on failure").** When a
   tier-declared task returns a *structured low-confidence/failed result*, retry
   it on the next stronger tier — explicit and feature-owned, never an invisible
   retry for normal chat turns (AS-042 clarified decision). The escalation must
   be logged with a reason and shown in `/route` and `/cost`. This needs a
   feature that actually produces such a structured result; wire it when the
   first one exists (a candidate: the semantic `/clean` / `/tidy` analyzers, or a
   model-using system sub-agent).

2. **Per-session `/route` overrides.** Let `/route` *set* a tier→model or
   feature→tier override for the current session (on top of the config policy),
   so a user can retune routing mid-session without editing config. The inspector
   already reads "active policy and per-session overrides"; this adds the
   mutation path. Keep config the durable source of truth; session overrides are
   transient.

## Acceptance criteria

- [ ] A tier-declared task that returns a structured low-confidence/failed result
      retries once on the next stronger tier; the escalation is logged with a
      reason.
- [ ] Escalations are visible in `/route` and `/cost` (which tier served, and that
      an escalation occurred and why).
- [ ] `/route <feature> <tier>` (and/or `/route <tier> <vendor> <model>`) sets a
      per-session override that `Resolve`/`FeatureTier` honor for the rest of the
      session, layered over the config policy.
- [ ] Per-session overrides reset on `/clear` and a fresh session; config is
      unchanged.
- [ ] Overrides do **not** mutate the shared config-owned `routing.Policy` maps
      (they are reference types — copying the struct by value shares them). Use a
      separate session override layer, or a `Policy.Clone()` deep copy, so the
      durable config policy stays untouched. *(Gemini review on #216.)*

## Dependencies

- AS-042 (the routing substrate, `internal/routing`, and the `/route` inspector)
