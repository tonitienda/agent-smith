---
id: AS-058
title: Self-improving config (aggregated insights propose memory/skill/command edits)
status: needs-clarification
github_issue: 58
depends_on: [AS-032, AS-045, AS-050]
area: insights-wedge
priority: P2
source: PRD.md §7.25
---

# AS-058 · Self-improving config

**Status: needs clarification**

## Description

§7.25: aggregate insights into proposed edits to memory, skills, and commands so the agent gets better at *your* workflow over time. Living skills (AS-048/049/050) is the first instance; this generalizes the pattern to all config surfaces. The PRD is intentionally thin here — it's the capstone, and most open questions resolve only after the upstream features generate real data.

## Open questions (why this needs clarification)

1. **Trigger & cadence** — proposals at session end, on a rollup schedule, or on demand (`/improve`)? When does evidence cross the proposal threshold (the same suggestion appearing in N sessions)?
2. **Scope of edit targets** — memory files and skills are covered by earlier tickets; do *custom commands* and *config* (permissions, MCP scoping like "this server returned 40k unused tokens — scope it") get auto-proposals too?
3. **Approval UX** — one queue of pending proposals (where? `/insights`? a dedicated `/improve` panel?) vs inline prompts; how do dismissals decay vs persist?
4. **Conflict handling** — proposals touching the same file/skill across sessions; dedupe and supersede rules.

## Acceptance criteria (draft, to confirm after clarification)

- [ ] A friction pattern recurring across ≥N sessions yields exactly one consolidated proposal with cross-session evidence.
- [ ] Every applied edit goes through diff preview; every proposal is dismissible with memory of the dismissal.
- [ ] Proposals never auto-apply (propose_edit permission semantics, Appendix C.5).
- [ ] Measurable claim: applying proposals reduces the measured friction in subsequent sessions (track via AS-030/AS-057 metrics).

## Dependencies

- AS-032 (memory targets), AS-045 (suggestion machinery), AS-050 (rollup evidence)
