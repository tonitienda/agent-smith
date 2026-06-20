---
id: AS-052
title: ACP server (editor / programmatic protocol face)
status: ready-to-implement
github_issue: 52
depends_on: [AS-018, AS-051]
area: faces
priority: P1
source: PRD.md §7.18, §5, §9, §10 Q5
---

# AS-052 · ACP server

**Status: ready to implement**

## Description

§7.18: speak Agent Client Protocol (or compatible) so editors and external clients can drive the same core; §5 makes ACP the path to the future desktop UI. The protocol question is **explicitly open (§10 Q5)** and the §9 risk table flags ACP spec immaturity.

Whatever the answer, the architecture work is the same and can be specified now: a protocol-agnostic adapter layer over the loop's face-agnostic events (AS-018 already enforces this), session lifecycle mapping, streaming, permission-request forwarding to the client.

## Clarified implementation decisions

- **Protocol plan:** AS-077 is the first programmatic transport: local JSON-RPC/WebSocket. This ticket implements an ACP-compatible adapter additively on top of the same protocol-agnostic session/event layer rather than replacing AS-077.
- **Spec pinning:** pin the ACP revision in documentation and tests at implementation time. Treat our adapter surface as additive-only where possible, but isolate spec churn behind the adapter package.
- **First target client:** prove the adapter with a minimal local conformance/client fixture first; a full editor integration can be AS-081 or a follow-on.
- **Permission UX:** permission requests are first-class protocol events. Clients that cannot answer them must choose an explicit deny-fast or preconfigured allow policy; silent approval is forbidden.

## Acceptance criteria

- [ ] An external client can start a session, stream a turn, see tool calls, and respond to permission requests over the chosen protocol.
- [ ] The protocol adapter contains no business logic (loop stays face-agnostic — enforced as in AS-018).
- [ ] One real editor integration works end-to-end.
- [ ] Personality stays off on this face (§7.21).

## Dependencies

- AS-018 (face-agnostic core), AS-051 (shares the programmatic plumbing)
