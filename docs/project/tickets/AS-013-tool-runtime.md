---
id: AS-013
title: Tool runtime framework (registry, validation, execution, logging)
status: ready-to-implement
github_issue: 13
depends_on: [AS-005]
area: tools
priority: P0
source: PRD.md §7.2, D6
---

# AS-013 · Tool runtime framework

**Status: ready to implement**

## Description

The framework concrete tools plug into (§7.2): registration, parameter schemas, execution, and result capture into the event log.

- Tool definition: name, description, JSON-schema parameters — exported to providers via AS-008.
- Execution context: working directory, environment, cancellation (context.Context), per-tool timeout.
- Input validation against the schema before execution; structured errors back to the model on invalid input.
- Results appended to the event log as `tool_result` blocks (with token counts once accounting lands); large outputs truncated with an explicit truncation marker.
- A permission-check hook point invoked before every execution (implemented by AS-016).

## Acceptance criteria

- [ ] Registering a tool makes it visible to providers and executable by the loop.
- [ ] Invalid arguments produce a model-readable error, not a crash.
- [ ] Cancellation kills an in-flight tool cleanly.
- [ ] Every execution leaves a `tool_call` + `tool_result` pair in the log with provenance.
- [ ] Permission hook is invoked on every execution path (test enforced).

## Dependencies

- AS-005 (results land in the event log)
