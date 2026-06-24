---
id: AS-057
title: Cross-session analytics (portfolio dashboard)
status: done
github_issue: 57
depends_on: [AS-007, AS-020, AS-045]
area: insights-wedge
priority: P2
source: PRD.md §7.24
---

# AS-057 · Cross-session analytics

**Status: done**

## Description

§7.24: a portfolio dashboard across sessions/projects — spend trends, recurring friction, "your top 3 ways to save money/time this week." The per-session machinery (insights findings, cost records, the rollup store from AS-050) provides the data; what's unspecified is the surface and the aggregation index.

## Clarified implementation decisions

- **Surface:** start with CLI/TUI textual reports (`smith stats` and a `/insights` cross-session view). Static HTML export is a follow-on once the data contract stabilizes.
- **Aggregation store:** maintain a local derived rollup index updated at session end, with an on-demand rebuild command from append-only session logs. The index is disposable derived state.
- **Recurring friction scope:** V1 only aggregates signals already emitted by `/insights`, `/skills`, costs, and session metadata. New detectors are follow-on tickets.
- **Scope/privacy:** default to current project. Cross-project aggregation is opt-in with a visible scope flag and never leaves the local machine.

## Acceptance criteria

- [x] Spend trend over time, per project and per model, from real session data.
- [x] At least one "top 3 savings" recommendation grounded in measured cross-session signals (§9 anti-generic rule applies here too).
- [x] Recurring-friction items link back to example sessions.
- [x] Works fully offline/local.

## Dependencies

- AS-007 (session corpus), AS-020 (cost records), AS-045/AS-050 (findings + rollup store)

## Implementation notes

- **Engine:** `internal/stats` is a pure, offline aggregation layer — `Build(sessions, friction, scope) → Report` and `Render(Report) → string`. Callers load the corpus and price each session through `internal/cost`; the engine folds spend per project/model/day, derives the top grounded savings, and projects recurring findings into friction items. Deterministic and unit-tested without fixtures.
- **Read path:** `session.AllSummaries(root)` and `session.OpenAt(dir)` (plus `Summary.Dir`, `Store.Root()`) added so the analytics can reach sessions across every project under the state root, not just one project-scoped store.
- **Surface:** delivered as a dedicated `stats` command (`smith stats` / `/stats`), the cross-session twin to single-session `/insights`. `/insights` stayed single-session (its `apply <n>` arg space is firmly per-session); overloading it was messier than a sibling command, so the "/insights cross-session view" from the clarified decision is `/stats`. `smith stats all` widens the spend view to all projects.
- **Aggregation index:** the report is recomputed from the append-only logs on every call (disposable derived state). The persisted session-end index + on-demand rebuild from the clarified decision, and cross-project friction merge, are deferred to **AS-136** as a performance follow-on — correctness does not need them, and the corpora are small in V1.
- **Friction linking:** `skillrollup.Group` gained an additive `Examples []string` (up to 3 session ids per finding) so recurring items link back to concrete sessions.
