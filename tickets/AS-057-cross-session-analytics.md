---
id: AS-057
title: Cross-session analytics (portfolio dashboard)
status: needs-clarification
github_issue: null
depends_on: [AS-007, AS-020, AS-045]
area: insights-wedge
priority: P2
source: PRD.md §7.24
---

# AS-057 · Cross-session analytics

**Status: needs clarification**

## Description

§7.24: a portfolio dashboard across sessions/projects — spend trends, recurring friction, "your top 3 ways to save money/time this week." The per-session machinery (insights findings, cost records, the rollup store from AS-050) provides the data; what's unspecified is the surface and the aggregation index.

## Open questions (why this needs clarification)

1. **Surface** — a TUI panel (`smith stats` / `/analytics`), a generated static HTML report, or both? (Local-first per D8 — no hosted dashboard — but HTML may serve "Team-Lead Tess" later.)
2. **Aggregation store** — scan session logs on demand (slow but zero new state) vs maintain a local index updated at session end (faster, but new derived state to keep additive-only)?
3. **Scope of "recurring friction"** — only what `/insights`/`/skills` already record, or new cross-session detectors (same file re-read across sessions, same error pattern)?
4. **Cross-project vs per-project** default, and any privacy toggles for aggregating across client codebases?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] Spend trend over time, per project and per model, from real session data.
- [ ] At least one "top 3 savings" recommendation grounded in measured cross-session signals (§9 anti-generic rule applies here too).
- [ ] Recurring-friction items link back to example sessions.
- [ ] Works fully offline/local.

## Dependencies

- AS-007 (session corpus), AS-020 (cost records), AS-045/AS-050 (findings + rollup store)
