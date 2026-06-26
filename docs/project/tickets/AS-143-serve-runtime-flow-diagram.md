---
id: AS-143
title: "Add smith serve JSON-RPC/WebSocket runtime flow diagram to runtime-flows.md"
status: done
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

## Resolution

1. **Worth adding now** — the diagram documents the architecture for AS-078 /
   AS-081 client implementors and for design reviews, so it lands now rather than
   waiting on a GUI to ship.
2. **Scope** — the diagram covers the happy-path turn over WebSocket *and* the
   architecturally interesting parts: server-initiated `event` notification
   streaming, `session.start` create/resume, and `permission.ask`
   forwarding with the fail-fast denial fallback.

Implemented as the "`smith serve` turn over JSON-RPC/WebSocket" sequence diagram
in [`runtime-flows.md`](../../architecture/runtime-flows.md), following the
existing mermaid `sequenceDiagram` style. Method names match `internal/serve`
(`session.start`, `turn.run`, `event`, `permission.ask`).
