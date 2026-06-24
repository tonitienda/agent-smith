---
id: AS-136
title: Persisted cross-session stats index + cross-project friction merge
status: done
github_issue: 416
depends_on: [AS-050, AS-057]
area: insights-wedge
priority: P3
source: AS-057 follow-on
---

# AS-136 · Persisted cross-session stats index + cross-project friction merge

## Problem

AS-057 ships the portfolio analytics (`smith stats` / `/stats`) by recomputing the
report from the append-only session logs on every invocation. That is correct and
fully offline, but it re-opens and re-prices every session each time, which grows
linearly with the corpus, and its recurring-friction view is limited to the current
project because the durable findings rollup (AS-050) is project-scoped.

The AS-057 clarified decision called for "a local derived rollup index updated at
session end, with an on-demand rebuild command from append-only session logs."
That index was deferred from AS-057 as a performance optimization; this ticket is
that index, plus the cross-project friction merge.

## What to build

- A disposable, derived stats index updated at session end (mirroring the
  `skillrollup.Store` pattern) so `smith stats` reads pre-aggregated per-session
  spend/model/day rows instead of re-pricing the whole corpus each call.
- An on-demand `smith stats rebuild` that reconstructs the index from the
  append-only logs, so the index is never load-bearing — a missing or stale index
  degrades to a full recompute (the current AS-057 behaviour).
- Cross-project friction in `smith stats all`: merge the per-project findings
  rollups (or index their groups) so recurring friction can span projects, with
  example session ids preserved for linking.

## Acceptance criteria

- [ ] `smith stats` reads the index when present and is measurably cheaper than a
      full recompute on a multi-session corpus.
- [ ] `smith stats rebuild` reconstructs the index from logs; deleting the index
      and re-running `smith stats` yields the same report (index is disposable).
- [ ] `smith stats all` surfaces recurring friction across projects with example
      session links.
- [ ] Still fully offline/local; the index never leaves the machine.

## Dependencies

- AS-050 (project-scoped findings rollup), AS-057 (the on-demand analytics this optimizes)
