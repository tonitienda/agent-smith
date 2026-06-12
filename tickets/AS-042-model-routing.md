---
id: AS-042
title: Model routing/tiering + /route command
status: needs-clarification
github_issue: null
depends_on: [AS-008, AS-031, AS-044]
area: cost
priority: P1
source: PRD.md §7.15, §5, Appendix A, §10 (adjacent)
---

# AS-042 · Model routing/tiering + /route

**Status: needs clarification**

## Description

§7.15: route mechanical subtasks (search, summarize, classify) to a cheap/fast model and reasoning to a strong model, with a configurable policy and auto-escalation on failure. §5 lists the router as a system sub-agent ("The Keymaker" in the theme layer).

What's clearly buildable: a **tier abstraction** (`cheap | standard | strong` mapped to concrete provider/models in config) consumed by everything that already declares a tier — `/compact` summarization (AS-038), system sub-agents (AS-044, Appendix C.3 `model: cheap`), subagent fan-out defaults (AS-046). `/route` inspects the policy and per-session overrides.

## Open questions (why this needs clarification)

1. **Does routing ever apply to the main interactive loop in v1**, or only to explicitly-tiered work (sub-agents, compaction, analyzers)? Auto-detecting "mechanical subtasks" inside the main loop is a much harder, riskier feature than tier mapping — the §6 guardrail (task success must not regress) is directly at stake.
2. **Auto-escalate on failure** — what is "failure"? Tool-error loops, user correction, explicit model admission? How many retries before escalating, and does the escalated attempt reuse the cheap attempt's output?
3. **Cross-provider routing** — may the cheap tier live on a different provider than the strong tier mid-session (the bilingual schema permits it; does policy)?
4. **Policy schema** — per-task-type rules, per-tool rules, or just tier defaults + per-feature tier declarations?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] Tiers are configurable; every tier-declaring feature resolves through the router.
- [ ] `/route` shows the active policy and which tier served each recent call.
- [ ] Escalation (once defined) is logged with reason and visible in `/cost`.
- [ ] §6 guardrail: routed sessions show no task-success regression on the AS-030 benchmark.

## Dependencies

- AS-008 (per-request model selection), AS-031 (policy config), AS-044 (router runs as a system sub-agent)
