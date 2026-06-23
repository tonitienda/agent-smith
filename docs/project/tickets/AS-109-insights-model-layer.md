---
id: AS-109
title: /insights model-assisted layer + goal anchoring (spun out of AS-045)
status: ready-to-implement
github_issue: 375
depends_on: [AS-045, AS-040, AS-042]
area: insights-wedge
priority: P2
source: PRD.md §7.14, §9, Appendix C.3; spun out of AS-045
---

# AS-109 · /insights model-assisted layer + goal anchoring

**Status: ready to implement**

## Description

AS-045 shipped `/insights` measured-first: a deterministic dashboard of measured
signals plus rule-based, grounded suggestions, with the insights-writer sub-agent
recording those suggestions at session end **with no model calls** (so the
cheap-tier/budget AC is trivially satisfied). That deliberately deferred the
**model-assisted layer** (PRD §7.14) — the part that turns the measured signals
into richer, model-authored suggestions ("this MCP returned 40k unused tokens —
scope it", "this span was a dead end — `/clean` it next time", "split this
skill") — and the goal anchoring.

This ticket adds, on top of the AS-045 substrate:

- A cheap-tier model pass in the insights-writer (Appendix C.3: `model: cheap`,
  `schedule: session_end`, `mode: async`) that takes the measured `insights.Report`
  as input and proposes additional suggestions. **§9 mitigation, non-negotiable:**
  every model-authored suggestion must still cite the measured evidence
  (turns/tokens/counts + `#<seq>` anchor) it is grounded in — never vibes — and
  must stay within the configured `budget.max_cost_usd_per_session`.
- Wire the writer onto the cheap routing tier once AS-042 lands; until then use the
  configured default model. Respect the `enabled` / `model` / `budget` config from
  the `subagents.insights_writer` overlay (already parsed by the registry).
- **Goal anchoring (AS-040, soft in AS-045):** when a `/goal` is set, the
  dashboard answers "did the session meet its objective?" using the goal block and
  the measured signals.
- The on-demand path: `/insights` on a session whose writer is disabled offers to
  run the (model) retro once on demand (the measured dashboard already renders for
  free regardless).

## Acceptance criteria

- [ ] With the model layer enabled, `/insights` produces ≥1 model-authored
      suggestion that cites measured evidence and a jump-to-transcript link.
- [ ] The model pass runs on the cheap tier, async at session end, and never
      exceeds the configured per-session budget; a disabled writer adds zero cost.
- [ ] The measured-signals dashboard (AS-045) still renders unchanged when the
      model layer is disabled.
- [ ] When a `/goal` is set, the dashboard reports whether the objective was met,
      grounded in measured signals.

## Dependencies

- AS-045 (measured substrate + writer + panel), AS-040 (goal anchoring), AS-042
  (model routing/tiering — soft; configured default until it lands)
