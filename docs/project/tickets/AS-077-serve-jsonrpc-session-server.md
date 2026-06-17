---
id: AS-077
title: "`smith serve` — local JSON-RPC/WebSocket session server"
status: ready-to-implement
github_issue: null
depends_on: [AS-018, AS-051]
area: faces
priority: P1
source: PRD.md §5, §7.18, §10 Q5; GUI grilling session 2026-06
---

# AS-077 · `smith serve` — local session server

**Status: ready to implement**

## Description

The spine for every graphical face. A coding agent's core job — run shell,
read/write files, hold API keys (D9/AS-017) — needs a real host machine; a
browser sandbox cannot do it. So instead of compiling the *live* agent to WASM,
we expose the existing **face-agnostic core** (AS-018) over a local programmatic
transport, and let thin clients (web GUI AS-078, Viscose extension AS-081) drive
it. One core, many skins.

Per PRD §10 Q5, the transport decision is resolved (GUI grilling): **ship a
minimal JSON-RPC surface over WebSocket now**, reusing the AS-051 headless
plumbing, rather than blocking on ACP spec churn (§9 risk). The adapter stays
protocol-agnostic so AS-052 (ACP) can be added later, additively (D2) — `serve`
is the JSON-RPC fallback that §9's mitigation calls for, and the substrate ACP
will eventually re-skin.

`smith serve` binds to **localhost only** by default. It is the single-user
local daemon; multi-tenant / stranger-facing hosting is explicitly out of scope
and tracked separately as the sandboxing spike AS-080 (D9: "not a sandbox").

## Scope

- `smith serve [--addr 127.0.0.1:PORT]` starts a server exposing the core over
  JSON-RPC 2.0 framed on a WebSocket. Localhost bind by default; refuse a
  non-loopback bind without an explicit `--unsafe-bind` opt-in that documents
  the AS-080 caveat.
- **Methods** map 1:1 onto the AS-018 loop seams and AS-051 session model:
  start/resume a session, send a turn, cancel a turn, list/load sessions.
- **Server→client notifications** are the face-agnostic `loop.UIEvent` stream
  (turn progress, tool-call start/result, assistant text deltas, cost updates)
  — the *same* events the TUI and headless faces consume. No business logic in
  the adapter (enforced as in AS-018/AS-052).
- **Permission requests** (AS-016 ask-mode) are forwarded to the client as a
  request the client answers; a client that cannot render them falls back to the
  AS-051 headless deny-fast posture (D-CLI-9), never hangs.
- Single shared command registry (AS-066) so slash/subcommand/RPC stay in parity.
- Personality stays off on this face (§7.21).

## Acceptance criteria

- [ ] A client can connect over WebSocket, start a session, send a turn, and
      receive the streamed `UIEvent` sequence (text, tool calls, results, cost).
- [ ] Tool calls execute through the normal runtime (AS-013) and permission gate
      (AS-016); an ask-mode prompt is forwarded to the client and the client's
      answer resumes or denies the call.
- [ ] Cancellation of an in-flight turn works over the wire.
- [ ] Sessions created via `serve` are normal sessions: resumable in the TUI and
      visible to `/insights` (parity with AS-051).
- [ ] The protocol adapter contains no business logic; the loop stays
      face-agnostic (same contract test discipline as AS-018).
- [ ] Default bind is loopback; a non-loopback bind requires `--unsafe-bind` and
      emits the AS-080 warning.
- [ ] No decorative/personality output on this path (§7.21).

## Non-goals

- Multi-tenant / remote / stranger-facing hosting and the sandboxing it demands
  → AS-080.
- Full ACP conformance → AS-052 (this surface is the JSON-RPC fallback it builds
  on).

## Dependencies

- AS-018 (face-agnostic core), AS-051 (headless programmatic plumbing it reuses),
  AS-066 (shared command registry, for parity).
