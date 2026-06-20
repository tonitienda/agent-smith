---
id: AS-081
title: Viscose (VS Code) extension over `smith serve`
status: ready-to-implement
github_issue: 136
depends_on: [AS-077, AS-078]
area: faces
priority: P2
source: PRD.md §5, §7.18; GUI grilling session 2026-06
---

# AS-081 · Viscose (VS Code) extension

**Status: ready to implement**

## Description

The editor face. A VS Code extension that drives the same core through
`smith serve` (AS-077) and renders the AS-078 web UI inside a webview — so the
extension is a *packaging + wiring* problem, not a second client. The VS Code
extension host is Node.js with full filesystem / process / network access, so
(unlike the browser) there is no capability gap to bridge; the only real
decision is **how the extension reaches the core**.

PRD §5 names the editor/desktop UI as the eventual home of this work; AS-052
(ACP) is the future-proof transport, but per the GUI grilling we ship on the
AS-077 JSON-RPC surface first and adopt ACP additively later.

## Clarified implementation decisions

- **Core wiring:** V1 locates an existing native `smith` binary from configured path or PATH and spawns `smith serve`. Bundled per-platform binaries are deferred; WASM-in-extension-host is out of scope.
- **Workspace integration depth:** V1 embeds the AS-078 web UI in a webview and adds thin VS Code bridges for opening files/diffs and permission prompts. It must not fork a second client UI.
- **Lifecycle:** one `smith serve` child process per workspace window, owned by the extension and stopped when the window closes unless the user configured an external server URL.
- **Packaging/distribution:** start with sideload/dev packaging; marketplace distribution and bundled binaries are follow-ons.

## Acceptance criteria

- [ ] The extension starts or connects to a `smith serve` instance and renders the
      AS-078 UI in a webview.
- [ ] A user can run a turn, see tool transparency, and answer permission prompts
      from inside VS Code.
- [ ] The wiring decision from Q1 is documented (and, if bundling, the
      build/distribution path is defined).
- [ ] Personality stays off where output is programmatic (§7.21).

## Dependencies

- AS-077 (`smith serve` transport), AS-078 (the web UI it embeds).
