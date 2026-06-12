---
id: AS-019
title: Parallel tool execution for independent calls
status: ready-to-implement
github_issue: null
depends_on: [AS-018]
area: loop
priority: P0
source: PRD.md §7.2
---

# AS-019 · Parallel tool execution

**Status: ready to implement**

## Description

When the model emits multiple tool calls in one assistant turn, execute independent ones concurrently (§7.2) — a direct speed win aligned with D5.

- Calls within a single assistant message run concurrently with a bounded worker pool.
- Results are appended to the event log in the original call order regardless of completion order (deterministic logs).
- Permission prompts serialize sensibly in `ask` mode (one prompt at a time; a denial doesn't cancel sibling approvals already granted).
- A failing tool doesn't cancel its siblings; each call gets its own result/error.

## Acceptance criteria

- [ ] Two slow independent tools complete in ~max(t1, t2), not t1 + t2 (timing test with fakes).
- [ ] Log order of results matches call order in all interleavings (stress test).
- [ ] Cancellation aborts all in-flight siblings cleanly.
- [ ] Ask-mode prompting remains coherent (no interleaved prompt garbage).

## Dependencies

- AS-018 (loop)
