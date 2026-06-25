---
id: AS-143
title: "Add smith serve JSON-RPC/WebSocket runtime flow diagram to runtime-flows.md"
status: needs-clarification
area: architecture
priority: low
depends_on: [AS-077]
---

# AS-143 — Add `smith serve` JSON-RPC/WebSocket runtime flow to runtime-flows.md

## Situation

`docs/architecture/runtime-flows.md` documents the four most important execution
paths: interactive TUI turn, `/clean` preview/apply, session resume, and headless
`smith run`. The `internal/serve` JSON-RPC/WebSocket face (AS-077) is a
fully-implemented fifth face but has no sequence diagram. The component and
container views do describe `serve` at their level, but the sequence-level flow is
absent.

The serve path has distinctive structure:
- A WebSocket client sends a `smith/run` JSON-RPC request.
- `cmd/smith` (the `serve.Backend` implementor) builds a loop turn and runs it.
- UI events stream back as server-initiated JSON-RPC notifications.
- Permission prompts travel as server-initiated request/response pairs (fail-fast
  if the client cannot answer).

## Open questions

1. **Is the diagram worth adding now?** The serve face is complete (AS-077,
   AS-119) but the consumer-facing GUI (AS-078) and VS Code extension (AS-081)
   are still `ready` tickets. The serve path's audience is limited until a GUI
   lands. Alternatively, the diagram is useful for implementors of future clients
   and for verifying the architecture during design reviews.

2. **Scope**: Should the diagram cover only the happy-path turn-over-WebSocket, or
   also permission-prompt forwarding and session create/resume flows?

## Suggested resolution

If the diagram is worth adding now, implement it in `runtime-flows.md` following
the existing mermaid `sequenceDiagram` style. The happy-path diagram is
straightforward; permission-prompt forwarding is the architecturally interesting
part (server-initiated request, fail-fast denial fallback).

If deferred, close this ticket when AS-078 (web GUI) is `ready-to-implement` and
reopen it there.
