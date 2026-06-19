---
id: AS-088
title: Wire the sub-agent Runner into the turn loop lifecycle
status: ready-to-implement
github_issue: null
depends_on: [AS-044, AS-018]
area: subagents
source: PRD.md §7.19, AS-044
---

# AS-088 · Wire the sub-agent Runner into the turn loop lifecycle

**Status: ready to implement**

## Description

AS-044 shipped the sub-agent framework as a self-contained package
(`internal/subagent`): a registry, a manifest contract, an in-memory insights
Store, and a lifecycle `Runner` with `Begin(scope)` / `Observe(block)` /
`End(scope, slice)`. It is pure and face-agnostic and does not import the loop —
the same way `budget.Guard` landed before the loop opted it in (AS-041).

This ticket opts the loop in. The turn loop (`internal/loop`) should drive an
optional `*subagent.Runner` at the lifecycle points the framework expects:

- `Begin` a span scope at the start of a turn/tool-batch span and the session
  scope at session start;
- `Observe` each block as it is appended to the log (passively — the Runner's
  `Observe` is already cheap and makes no model calls);
- `End` the span scope at span teardown and the session scope at session end,
  off the interactive hot path (a goroutine or a post-turn step), so teardown
  analysis never blocks the turn.

Follow the `WithBudget` precedent: a `WithSubAgents(runner)` option, a no-op when
nil, so a session with no sub-agents pays nothing.

## Acceptance criteria

- [ ] The loop drives `Begin`/`Observe`/`End` at the right lifecycle points when a
      Runner is installed; it is a no-op when none is.
- [ ] `Observe` runs inline (it is passive and cheap) but `End`/teardown work is
      kept off the turn's critical path.
- [ ] A test exercises the loop with a spy sub-agent and asserts it is inited,
      observed per block, and torn down — without a model call.

## Dependencies

- AS-044 (the framework + Runner this wires in), AS-018 (the loop lifecycle points)
