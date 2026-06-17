---
id: AS-078
title: Web GUI — thin client over `smith serve`
status: ready-to-implement
github_issue: 133
depends_on: [AS-077]
area: faces
priority: P1
source: PRD.md §5, §7.18; GUI grilling session 2026-06
---

# AS-078 · Web GUI thin client

**Status: ready to implement**

## Description

The "basic but functional" graphical face. A browser single-page app that drives
the **native** agent core through the AS-077 WebSocket — it holds no keys, runs
no tools, and contains no agent logic; it is a renderer for the face-agnostic
`UIEvent` stream and a sender of turns. All execution stays on the host machine
running `smith serve` (D9 posture preserved: your machine, your privileges).

This is deliberately *not* a WASM build of the agent. The pure-compute,
read-only observability views (`/context`, cost, composition, clean/compact
preview) that *do* compile to WASM live in the standalone inspector AS-079; this
ticket is the live, connected face.

The same client bundle is the substrate the Viscose extension (AS-081) reuses
inside a webview, so keep host-specific assumptions out of it.

## Scope

- Static SPA (vanilla or a light framework — keep the dependency footprint
  honest; no heavyweight SDKs) connecting to a configurable `smith serve`
  WebSocket endpoint.
- **Chat:** prompt input, streamed assistant text, turn lifecycle (thinking →
  tool calls → answer), cancel button.
- **Tool transparency (AS-024 parity):** render each tool call and its result —
  command/args, diff for edits, output (truncated like the TUI).
- **Permission prompts (AS-016):** when `serve` forwards an ask-mode request,
  render allow / allow-always / deny and send the answer back.
- **Cost meter (AS-020/063) + context meter (AS-025):** live from `UIEvent`s.
- **Session list / resume** via the AS-077 methods.

## Acceptance criteria

- [ ] From a browser, a user connects to a running `smith serve`, sends a prompt,
      and watches a turn stream to completion with tool calls and final answer.
- [ ] An ask-mode permission request renders in the UI and the user's choice
      drives the tool call (allow / allow-always / deny).
- [ ] Tool calls show command/args, edit diffs, and truncated output (AS-024
      parity).
- [ ] Live cost and context meters update during a turn.
- [ ] The client holds no API keys and issues no provider/tool calls itself —
      verified by inspecting network traffic (only the `serve` socket).
- [ ] Bundle builds to static assets servable by any static host; no backend of
      its own beyond `smith serve`.

## Non-goals

- Offline / no-daemon operation, and read-only session inspection → AS-079.
- Public multi-user hosting → AS-080.
- The VS Code packaging of this UI → AS-081.

## Dependencies

- AS-077 (`smith serve` transport). Renders AS-024 tool transparency, AS-016
  permissions, AS-020/AS-025 meters — parity with the TUI faces.
