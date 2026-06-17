---
id: AS-081
title: Viscose (VS Code) extension over `smith serve`
status: needs-clarification
github_issue: 136
depends_on: [AS-077, AS-078]
area: faces
priority: P2
source: PRD.md §5, §7.18; GUI grilling session 2026-06
---

# AS-081 · Viscose (VS Code) extension

**Status: needs clarification**

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

## Open questions (why this needs clarification)

1. **How does the extension reach the core?** Parked in the grilling — decide:
   - **Bundle/locate the native `smith` binary** and have the extension spawn
     `smith serve`, then talk AS-077 (laziest; reuses everything; cost is
     shipping/locating per-platform binaries).
   - **Spawn the user's already-installed `smith`** (no bundling; depends on PATH
     / a configured path).
   - **WASM core inside the extension host** (single cross-platform artifact, but
     reimplements fs/exec bridges Node gives for free — likely not worth it).
2. **Workspace integration depth for v1:** chat webview only, or also editor
   affordances (open the file a tool edited, show diffs in the diff view,
   surface permission prompts as VS Code modals)?
3. **Lifecycle:** one `serve` per workspace window? Reuse a running daemon?
   Startup/shutdown ownership.
4. **Packaging & distribution:** marketplace vs sideload for v1; per-platform
   binary matrix if bundling.

## Acceptance criteria (draft, confirm after clarification)

- [ ] The extension starts or connects to a `smith serve` instance and renders the
      AS-078 UI in a webview.
- [ ] A user can run a turn, see tool transparency, and answer permission prompts
      from inside VS Code.
- [ ] The wiring decision from Q1 is documented (and, if bundling, the
      build/distribution path is defined).
- [ ] Personality stays off where output is programmatic (§7.21).

## Dependencies

- AS-077 (`smith serve` transport), AS-078 (the web UI it embeds).
