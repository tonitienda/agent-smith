---
id: AS-175
title: Wails desktop packaging, signing, updates, and smoke tests
status: ready-to-implement
github_issue: null
depends_on: [AS-168, AS-169, AS-170]
area: quality
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-175 · Wails desktop packaging, signing, updates, and smoke tests

**Status: ready to implement**

## Description

Turn the Wails desktop app from a local dev shell into a releasable product
artifact with packaging, signing/update strategy, and basic smoke coverage for
the embedded-runtime flow.

This work is operationally important enough that it should not be deferred until
"after the UI is done."

## Scope

- Define the initial release targets and packaging path.
- Add signing/update plumbing appropriate to the chosen release channels.
- Add smoke tests for app boot, runtime launch/attach, and a basic interactive
  turn using fixture or local-test infrastructure where practical.
- Document release/operator steps.

## Acceptance criteria

- [ ] The project can produce installable desktop artifacts for the chosen first
      platforms.
- [ ] The release path documents signing/update requirements clearly.
- [ ] Smoke coverage exists for desktop boot and embedded-runtime initialization.
- [ ] A basic interactive session path is exercised in automated or
      fixture-driven validation.

## Non-goals

- Broad cross-platform visual regression coverage.
- A polished in-app update UX if manual update flow is temporarily required.

## Dependencies

- AS-168, AS-169, AS-170.
