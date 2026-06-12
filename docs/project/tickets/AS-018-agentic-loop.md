---
id: AS-018
title: Agentic loop orchestrator
status: ready-to-implement
github_issue: 18
depends_on: [AS-006, AS-008, AS-013]
area: loop
priority: P0
source: PRD.md §7.2, D6
---

# AS-018 · Agentic loop orchestrator

**Status: ready to implement**

## Description

The core turn loop tying everything together: user message → projection → provider stream → tool dispatch → results appended → continue until the model stops.

- Drive any AS-008 provider with the AS-006 projection as input each turn (context is always re-projected — never cached state).
- Append every block (user, assistant text, reasoning, tool calls/results) to the event log as it streams.
- Dispatch tool calls through the runtime (AS-013) and feed results back; loop until a final assistant message or a stop condition.
- Stop conditions: model end-turn, user cancel (Esc), max-iterations safety valve, provider error after retries.
- Emit face-agnostic UI events (text delta, tool started/finished, turn complete) consumed by the TUI — keep the loop UI-free so ACP/headless can reuse it later (§5).

## Acceptance criteria

- [ ] End-to-end session works with the mock provider in tests: multi-turn, multi-tool, streaming.
- [ ] Cancellation mid-stream and mid-tool leaves the log consistent (no orphaned call without a result or cancellation marker).
- [ ] The loop package has zero TUI imports.
- [ ] Max-iteration guard prevents runaway tool loops, surfaced as a clear stop reason.

## Dependencies

- AS-006 (projection), AS-008 (provider interface), AS-013 (tool runtime)
