---
id: AS-182
title: Desktop settings, runtime status, and auth guidance
status: ready-to-implement
github_issue: null
depends_on: [AS-177, AS-178, AS-017]
area: faces
priority: P2
source: docs/project/smith-desktop-wails-prd.md
---

# AS-182 · Desktop settings, runtime status, and auth guidance

**Status: ready to implement**

## Description

Add a minimal settings/runtime surface so the desktop app can explain what
environment it is connected to, what provider/auth state exists, and how to fix
common setup problems.

For the first version this should emphasize clarity over full in-app account
management.

## Scope

- Runtime status view.
- Display active endpoint/mode information.
- Show auth readiness hints derived from Smith's existing auth state where
  possible.
- Link or route the user toward the correct setup/recovery path.

## Acceptance criteria

- [ ] A user can inspect whether the desktop app is connected to a local Smith
      runtime and whether it is healthy.
- [ ] Common auth/setup problems produce actionable guidance.
- [ ] The desktop app does not invent a parallel secret store or auth model.

## Non-goals

- Full key-entry UI for every provider.
- Enterprise/team settings.

## Dependencies

- AS-177, AS-178, AS-017.
