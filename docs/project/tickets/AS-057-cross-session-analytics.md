---
id: AS-057
title: Cross-session analytics (portfolio dashboard)
status: ready-to-implement
github_issue: 57
depends_on: [AS-007, AS-020, AS-045]
area: insights-wedge
priority: P2
source: PRD.md §7.24
---

# AS-057 · Cross-session analytics

**Status: ready to implement**

## Description

§7.24: a portfolio dashboard across sessions/projects — spend trends, recurring friction, "your top 3 ways to save money/time this week." The per-session machinery (insights findings, cost records, the rollup store from AS-050) provides the data; what's unspecified is the surface and the aggregation index.

## Clarified implementation decisions

- **Surface:** start with CLI/TUI textual reports (`smith stats` and a `/insights` cross-session view). Static HTML export is a follow-on once the data contract stabilizes.
- **Aggregation store:** maintain a local derived rollup index updated at session end, with an on-demand rebuild command from append-only session logs. The index is disposable derived state.
- **Recurring friction scope:** V1 only aggregates signals already emitted by `/insights`, `/skills`, costs, and session metadata. New detectors are follow-on tickets.
- **Scope/privacy:** default to current project. Cross-project aggregation is opt-in with a visible scope flag and never leaves the local machine.

## Acceptance criteria

- [ ] Spend trend over time, per project and per model, from real session data.
- [ ] At least one "top 3 savings" recommendation grounded in measured cross-session signals (§9 anti-generic rule applies here too).
- [ ] Recurring-friction items link back to example sessions.
- [ ] Works fully offline/local.

## Dependencies

- AS-007 (session corpus), AS-020 (cost records), AS-045/AS-050 (findings + rollup store)
