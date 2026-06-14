---
id: AS-013
title: Tool runtime framework (registry, validation, execution, logging)
status: done
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

- [x] Registering a tool makes it visible to providers and executable by the loop. — `Registry.Register` keys tools by name (rejecting duplicates/empty names); `Registry.ProviderDefs()` renders them as `provider.ToolDef` (client kind) in stable name order, and `Runtime.Execute` looks them up by the call's tool name.
- [x] Invalid arguments produce a model-readable error, not a crash. — `validateArgs` checks the call against a pragmatic JSON-Schema subset before execution; a failure becomes a `tool_result` with `IsError` and a nil Go error so the loop feeds it back to the model (`TestExecuteInvalidArgumentsDoesNotCrashOrRun`).
- [x] Cancellation kills an in-flight tool cleanly. — each run gets a child context bounded by the per-tool budget; a fired budget yields a model-readable "timed out" result, while parent-`ctx` cancellation propagates as a Go error and records nothing (`TestExecutePerToolTimeoutIsModelReadable`, `TestExecuteParentCancellationPropagates`).
- [x] Every execution leaves a `tool_call` + `tool_result` pair in the log with provenance. — `Execute` ensures the call is on the log (appending if absent, idempotent with a loop that already recorded it) and appends the `tool_result` linked by `ToolUseID` and `Provenance.DerivedFrom` (`TestExecuteHappyPathLogsPair`, `TestExecuteIdempotentWithPreloggedCall`).
- [x] Permission hook is invoked on every execution path (test enforced). — the single `Run` call site is gated by `r.permission`; `TestPermissionHookInvokedOnEveryExecution` asserts the gate is consulted and a denial blocks execution with a model-readable error.

## Implementation notes

- Package `internal/tool`: `tool.go` (the `Tool` interface, `Def`, `Output`, the `Func` adapter), `registry.go` (`Registry` → `provider.ToolDef`), `runtime.go` (`Runtime.Execute`: validate → permission → run → truncate → log), `permission.go` (`PermissionFunc`/`Decision`, `AllowAll` default), `validate.go` (minimal JSON-Schema argument validation).
- **Error discipline**: a failure the model should react to (unknown tool, invalid args, denied permission, per-tool timeout, tool domain error) is a `tool_result` with `IsError` + nil Go error; only an infrastructure failure or turn cancellation returns a Go error.
- **No mutable runtime state** after construction, so `Execute` is concurrency-safe for the parallel-tool turns of AS-019.
- **Permission gate** is a stdlib hook point (`AllowAll` default) that AS-016 fills with the real ask/allowlist/auto policy.
- **Truncation**: oversized text content is cut at `WithMaxResultBytes` (default 32 KiB) with an explicit marker part and `ToolResultBody.Truncated`; non-text parts are preserved.
- Follow-on surfaced as a ticket, not a TODO: **AS-062** — fuller JSON-Schema argument validation (the V1 validator covers the common object/required/scalar-type/`additionalProperties` cases and is deliberately lenient about the rest).

## Dependencies

- AS-005 (results land in the event log)
- AS-008 (tool definitions are exported to providers as `provider.ToolDef`)
