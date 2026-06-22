---
id: AS-117
title: /tidy dead-end collapse + working-memory promotion (spun out of AS-043)
status: needs-clarification
github_issue: null
depends_on: [AS-043, AS-048]
area: context-wedge
priority: P2
source: PRD.md §7.13, §9; AS-043 clarified decisions
---

# AS-117 · /tidy dead-end collapse + working-memory promotion

**Status: needs clarification**

## Description

AS-043 shipped the mechanical, zero-token half of `/tidy`: dedupe of identical
file reads (keep latest, drop older) behind a reversible exclusion event and a
fidelity diff. The two richer halves of §7.13 were deliberately deferred so they
land as a separate, reviewed change rather than a second silent removal path
(D0):

- **Dead-end collapse** — group a messy session by phase/turn/file/tool span and
  surface heuristic dead ends (repeated failing shell commands, abandoned file
  paths) as preview candidates. Per the AS-043 clarified decision this is
  **user-assisted and heuristic**: no autonomous deletion — the preview decides,
  and removal still flows through the same reversible exclusion mechanism tidy
  already uses.
- **Working-memory promotion** — promote concrete durable facts (commands, paths,
  config values, repo conventions — the same inclusion set as AS-048) into a
  labeled "working memory" block. The AS-043 decision is explicit that this must
  **reuse AS-048's single memory-writing path**, not invent a second one; `/tidy`
  may surface candidates but the write goes through the shared mechanism.

## Open questions

- **Dead-end heuristics:** what exact signals qualify a span as a dead end
  (e.g. N consecutive failing `shell` results on the same command; a file read
  then never referenced again; a tool error loop)? Reuse the AS-045/AS-048
  signal definitions where they already exist rather than defining new ones.
- **Working-memory block shape:** is the promoted block a derived `KindText`
  system block, or does it ride on the AS-048 memory-file write? The §7.13 wedge
  wants it *in the window*; AS-048 writes to a file picked up next session — these
  are different surfaces and the relationship must be pinned before building.
- **Composition with dedup:** when dead-end collapse and dedup both apply in one
  `/tidy`, is it one combined fidelity diff + one exclusion event, or staged
  steps? (Lean: one diff, one atomic event, mirroring the dedup path.)

## Dependencies

- AS-043 (the `/tidy` shell, fidelity-diff preview, exclusion/undo mechanics).
- AS-048 (the rediscovered-fact inclusion set + the single memory-writing path
  working-memory promotion must reuse).
