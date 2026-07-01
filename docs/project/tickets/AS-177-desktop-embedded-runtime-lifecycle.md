---
id: AS-177
title: Desktop embedded runtime lifecycle and app state
status: ready-to-implement
github_issue: null
depends_on: [AS-176, AS-077]
area: faces
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-177 · Desktop embedded runtime lifecycle and app state

**Status: ready to implement**

## Description

Make the desktop app feel self-contained by owning the Smith runtime lifecycle
inside one packaged application.

This ticket is the lifecycle contract between the Wails shell/adapter and the
embedded Smith runtime. It must be explicit, debuggable, and strict about the
shell/core boundary.

## Scope

- Initialize the Smith runtime inside the desktop app process.
- Detect health/readiness for the desktop session layer.
- Recover cleanly from transient adapter/runtime errors when safe.
- Surface actionable runtime failure states in the UI.

## Acceptance criteria

- [ ] From a fresh app launch, the desktop app can initialize the Smith runtime
      without manual terminal setup.
- [ ] Runtime startup, ready, degraded, and failed states are surfaced
      clearly to the user.
- [ ] A transient desktop-adapter/runtime fault can be retried without
      restarting the entire app when the runtime is still healthy.
- [ ] Embedded mode does not move provider/tool/session logic into Wails.

## Non-goals

- Packaging/updater.
- Session UI beyond a basic connection state.

## Dependencies

- AS-176.
