---
id: AS-188
title: Remote-steer & mobile-friendly control surface over smith serve
status: Pending Debrief
github_issue: null
area: faces
priority: P2
depends_on: [AS-077, AS-078, AS-171, AS-155]
source: docs/project/competitors.md
---

# AS-188 · Remote-steer & mobile-friendly control surface over `smith serve`

## Description

AS-171 scoped **outbound completion notifications** for background and
orchestrator runs and explicitly deferred the harder question in its
debrief section: "should the operator API expose a 'remote steer' action
... or is that explicitly out of scope until a GUI/mobile face exists?"

That question now has fresh competitive evidence. In the same two-week
window, OpenAI's Codex Remote reached GA (approve/steer a run from ChatGPT
mobile against a paired desktop host), Cursor's iOS app entered public beta
(Remote Control of desktop agents, live-activity status, mobile diff/log
review), and Sourcegraph Amp shipped a mobile watch/drive view. Three
independent vendors converged on "walk away and steer from a phone" within
weeks of each other — this is no longer a single vendor's mobile bet.

Smith already has the right building block: `smith serve` (AS-077, done)
exposes a local JSON-RPC/WebSocket session server, and the web GUI thin
client (AS-078) is the planned browser face over it. This ticket scopes
**remote steering** as a capability of that existing face — responding to a
pending permission gate, sending a follow-up instruction to a running
session, and viewing live tool/diff activity from a phone browser — rather
than a new mobile app or a new transport.

Consistent with D9, this stays local-first: `smith serve` remains a local
server a user's own device(s) connect to (e.g. over a Tailscale/VPN/LAN
reach the user controls), not a hosted multi-tenant service. D9's
"not a sandbox" posture and the AS-080 resolution (no hosting strangers'
agents) are unaffected — this is the same user, a second device.

## Acceptance criteria

- [ ] The web GUI thin client (AS-078) or a documented minimal subset of it
      renders usably on a mobile viewport: live transcript, pending
      permission gate, and a way to send a follow-up message.
- [ ] A pending permission gate can be approved/denied remotely through
      `smith serve`'s existing RPC surface (or an addition to it), sourced
      from the same event-log state the TUI reads — no parallel state.
- [ ] Remote actions are attributed in the event log the same way local
      actions are (D3) — a remote approval is indistinguishable in
      provenance from a local one except for its origin metadata.
- [ ] Docs cover the intended reach model (same-LAN, Tailscale/VPN, or a
      user's own reverse proxy) and explicitly do not recommend exposing
      `smith serve` to the public internet.
- [ ] Builds on, and does not duplicate, AS-171's notification schema —
      a remote-steer action is a response to a notification-worthy state,
      not a separate tracking mechanism.

## Debrief questions

- Does remote-steer belong on `smith serve`/web GUI (AS-077/078) or on the
  orchestrator's operator API (AS-155)? Local async runs and orchestrator
  daemon runs currently have separate completion-surface stories (also
  flagged by AS-171); this ticket assumes the web GUI face since that's
  where a human read/review loop already lives, but that should be
  confirmed.
- Is a dedicated mobile-optimized view worth building now, or is "the
  existing web GUI happens to be usable on a phone" sufficient for a first
  version, deferring a purpose-built mobile layout?
- What's the minimum viable reach story — does Smith need to document/ship
  anything beyond "point your phone's browser at your machine's LAN/VPN
  address," or is a lightweight pairing/QR flow (as Codex Remote uses)
  worth scoping now to reduce setup friction?

## Dependencies

[AS-077, AS-078, AS-171, AS-155]
