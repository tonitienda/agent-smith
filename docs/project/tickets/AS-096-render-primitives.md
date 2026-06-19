---
id: AS-096
title: Add tiny shared render primitives for textual reports
status: ready-to-implement
github_issue: 166
depends_on: []
area: polish
priority: P2
source: code-improvements.md
---

# AS-096 · Add tiny shared render primitives for textual reports

**Status: ready to implement**

## Description

Several feature packages render plain textual reports: cost, composition,
compact, clean, budget, goal, and future insight commands. Some duplication is
healthy, but formatting details such as currency, token counts, timestamps,
empty states, and tabwriter usage should be consistent.

Add a very small `internal/render` package for generic formatting primitives
only. Do not move feature-specific report logic into it; each feature keeps its
own `Render` function.

## Acceptance criteria

- [ ] Shared helpers cover common token/count, dollar, timestamp, and table
      formatting used by at least three report renderers.
- [ ] Feature-specific rendering remains in feature packages.
- [ ] Golden tests are updated or added to lock output stability.
- [ ] The new package has no external dependencies.

## Dependencies

- None
