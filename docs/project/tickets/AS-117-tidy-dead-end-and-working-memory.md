---
id: AS-117
title: /tidy dead-end collapse + working-memory promotion (spun out of AS-043)
status: done
github_issue: 382
depends_on: [AS-043, AS-048]
area: context-wedge
priority: P2
source: PRD.md §7.13, §9; AS-043 clarified decisions
---

# AS-117 · /tidy dead-end collapse + working-memory promotion

**Status: done**

## Implementation notes

- **Dead-end collapse** lives in `internal/tidy/deadend.go`, folded into
  `tidy.Preview`/`Plan` so it rides the *same* `KindExclusion` event and fidelity
  diff as the dedup half — one preview→apply/undo cycle (`/tidy` · `--apply` ·
  `--undo`), no second removal path. Two high-precision signals per the AS-043
  clarified decision: (1) a shell command that failed **more than once** (keep the
  latest failure, drop the earlier identical ones), and (2) a file read the thread
  visibly moved on from — a later grep/glob still hunts it by name and the exact
  path is never referenced again (the later-search requirement is the precision
  bar so an ordinary kept read is never collapsed). Recent reads are left alone.
- **Working-memory promotion** is surfaced in the same `/tidy` preview but the
  write is deferred to AS-048's single memory-writing path: `tidyPreview` runs the
  `factdetector` (AS-048) over the live window with the shared `saveTargetResolver`
  and `/tidy --promote [<n>]` writes accepted facts via the same
  `resolveApplyTarget`/`appendMemoryLine` helpers `/improve apply` uses — no second
  memory-writing path is introduced.

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

## Clarification (resolved 2026-06-30)

The three open questions below are already answered by the AS-043 "Clarified
implementation decisions" (done) and AS-048's shipped behavior (done); nothing
new needs deciding before implementation:

- **Dead-end heuristics:** AS-043's clarified decisions already pin this down —
  "group by phase/turn/file/tool spans, detect repeated failing shell commands
  and abandoned file paths, then let the preview decide. No autonomous
  deletion." That *is* the AS-045/AS-048-style signal definition this ticket
  asked to reuse: (1) repeated failing `shell` results on the same command
  within a span, and (2) a file read whose path is never referenced again
  later in the session. No third "tool error loop" signal is needed for V1 —
  it collapses into (1) for shell and is out of scope for other tools.
- **Working-memory block shape:** AS-043 is explicit: "the promotion mechanism
  is shared with AS-048; `/tidy` may surface candidates but must not invent a
  second memory-writing path." So the promoted fact is **not** a derived
  `KindText` window block — it rides on AS-048's existing memory-file write
  (skill scope → deepest memory file → project-root fallback, per AS-048's
  `Resolve` func), applied via the same diff-preview AS-048/AS-045 already use.
  `/tidy` surfaces candidates in its fidelity diff but defers the actual write
  to that single path, exactly as AS-043 already shipped for the dedupe half.
- **Composition with dedup:** AS-043's own lean — "one diff, one atomic event,
  mirroring the dedup path" — is the answer: dead-end collapse extends the same
  `KindExclusion` event/fidelity-diff AS-043 ships for dedupe, combined into one
  preview→apply/undo cycle rather than staged steps.

## Dependencies

- AS-043 (the `/tidy` shell, fidelity-diff preview, exclusion/undo mechanics).
- AS-048 (the rediscovered-fact inclusion set + the single memory-writing path
  working-memory promotion must reuse).
