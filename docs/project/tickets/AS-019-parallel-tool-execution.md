---
id: AS-019
title: Parallel tool execution for independent calls
status: done
github_issue: 19
depends_on: [AS-018]
area: loop
priority: P0
source: PRD.md §7.2
---

# AS-019 · Parallel tool execution

**Status: done**

## Description

When the model emits multiple tool calls in one assistant turn, execute independent ones concurrently (§7.2) — a direct speed win aligned with D5.

- Calls within a single assistant message run concurrently with a bounded worker pool.
- Results are appended to the event log in the original call order regardless of completion order (deterministic logs).
- Permission prompts serialize sensibly in `ask` mode (one prompt at a time; a denial doesn't cancel sibling approvals already granted).
- A failing tool doesn't cancel its siblings; each call gets its own result/error.

## Acceptance criteria

- [x] Two slow independent tools complete in ~max(t1, t2), not t1 + t2 (timing test with fakes).
- [x] Log order of results matches call order in all interleavings (stress test).
- [x] Cancellation aborts all in-flight siblings cleanly.
- [x] Ask-mode prompting remains coherent (no interleaved prompt garbage).

## Implementation notes

`tool.Runtime.ExecuteBatch` (internal/tool/runtime.go) runs a turn's client tool
calls in three phases:

1. **Gate, serially in call order** — cancellation check, argument validation, and
   the permission hook run one call at a time, so ask-mode prompts never
   interleave and a denial leaves an already-approved sibling untouched.
2. **Run, concurrently** — approved tools execute under a bounded worker pool
   (`WithMaxParallel`, default `DefaultMaxParallel = 8`); a failing or timing-out
   tool records its own error result and does not cancel its siblings.
3. **Record, in call order** — results are appended to the log deterministically
   regardless of which tool finished first.

The loop's `dispatch` (internal/loop/loop.go) calls `ExecuteBatch` with
`BatchHooks` that emit `UIToolStarted`/`UIToolFinished` per call in order; on
cancellation it reconciles every still-unanswered call with a cancellation
marker, preserving the AS-018 no-orphan invariant. `provider.ToolCallsTurn`
scripts multi-call turns for tests.

## Dependencies

- AS-018 (loop)
