---
id: AS-052
title: ACP server (editor / programmatic protocol face)
status: needs-clarification
github_issue: 52
depends_on: [AS-018, AS-051]
area: faces
priority: P1
source: PRD.md §7.18, §5, §9, §10 Q5
---

# AS-052 · ACP server

**Status: needs clarification**

## Description

§7.18: speak Agent Client Protocol (or compatible) so editors and external clients can drive the same core; §5 makes ACP the path to the future desktop UI. The protocol question is **explicitly open (§10 Q5)** and the §9 risk table flags ACP spec immaturity.

Whatever the answer, the architecture work is the same and can be specified now: a protocol-agnostic adapter layer over the loop's face-agnostic events (AS-018 already enforces this), session lifecycle mapping, streaming, permission-request forwarding to the client.

## Open questions (why this needs clarification)

1. **§10 Q5 verbatim:** commit to Agent Client Protocol now, or ship a minimal JSON-RPC surface first and adopt ACP once stable? (§9 mitigation suggests: abstract the protocol layer, ship JSON-RPC fallback, track the spec.)
2. **Spec version pinning** — if ACP: which revision, and what's the compatibility policy while the spec churns (does additive-only D2 discipline extend to our protocol surface)?
3. **First target client** — which editor integration proves it (Zed is the natural ACP candidate)? Defines the conformance bar.
4. **Permission UX over the wire** — how do ask-mode prompts map to protocol messages, and what happens with a client that can't render them (fall back to headless deny-fast behavior)?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] An external client can start a session, stream a turn, see tool calls, and respond to permission requests over the chosen protocol.
- [ ] The protocol adapter contains no business logic (loop stays face-agnostic — enforced as in AS-018).
- [ ] One real editor integration works end-to-end.
- [ ] Personality stays off on this face (§7.21).

## Dependencies

- AS-018 (face-agnostic core), AS-051 (shares the programmatic plumbing)
