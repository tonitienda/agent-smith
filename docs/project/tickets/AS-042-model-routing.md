---
id: AS-042
title: Model routing/tiering + /route command
status: ready-to-implement
github_issue: 42
depends_on: [AS-008, AS-031, AS-044]
area: cost
priority: P1
source: PRD.md §7.15, §5, Appendix A, §10 (adjacent)
---

# AS-042 · Model routing/tiering + /route

**Status: ready to implement**

## Description

§7.15: route mechanical subtasks (search, summarize, classify) to a cheap/fast model and reasoning to a strong model, with a configurable policy and auto-escalation on failure. §5 lists the router as a system sub-agent ("The Keymaker" in the theme layer).

What's clearly buildable: a **tier abstraction** (`cheap | standard | strong` mapped to concrete provider/models in config) consumed by everything that already declares a tier — `/compact` summarization (AS-038), system sub-agents (AS-044, Appendix C.3 `model: cheap`), subagent fan-out defaults (AS-046). `/route` inspects the policy and per-session overrides.

## Clarified implementation decisions

- **Scope:** V1 routing applies only to explicitly tier-declared work: compaction, semantic/tidy analyzers, system sub-agents, and user sub-agent defaults. It does not auto-downgrade the main interactive loop.
- **Escalation:** V1 escalation is explicit and feature-owned: a tier-declared task may retry on the next stronger tier only when it returns a structured low-confidence/failed result. No invisible retries for normal chat turns.
- **Cross-provider policy:** allowed when config maps tiers to different providers; every provider/model switch is logged and visible in `/route` and `/cost`.
- **Policy schema:** tier defaults plus per-feature overrides keyed by feature/sub-agent name. Per-tool and intent-classifier policies are deferred.

## Acceptance criteria

- [ ] Tiers are configurable; every tier-declaring feature resolves through the router.
- [ ] `/route` shows the active policy and which tier served each recent call.
- [ ] Escalation (once defined) is logged with reason and visible in `/cost`.
- [ ] §6 guardrail: routed sessions show no task-success regression on the AS-030 benchmark.

## Dependencies

- AS-008 (per-request model selection), AS-031 (policy config), AS-044 (router runs as a system sub-agent)
