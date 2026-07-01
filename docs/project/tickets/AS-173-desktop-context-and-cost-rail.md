---
id: AS-173
title: Desktop context and cost rail
status: ready-to-implement
github_issue: null
depends_on: [AS-170, AS-025, AS-020, AS-063]
area: faces
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-173 · Desktop context and cost rail

**Status: ready to implement**

## Description

Surface Smith's context and cost wedge in a compact desktop-native form during
interactive use. The first version does not need the full inspect-mode depth,
but it must keep users aware of context pressure and spend while the session is
in progress.

## Scope

- Always-visible compact context meter.
- Live token/cost summary for the active session.
- Lightweight detail panel for the current turn/session totals.
- Clear status when pricing or context data is unavailable.

## Acceptance criteria

- [ ] The desktop interactive view shows live context pressure and session cost.
- [ ] Meter and totals update as turns progress.
- [ ] Missing/unpriced data is represented explicitly rather than as zero.
- [ ] The UI reuses existing Smith accounting/composition semantics.

## Non-goals

- Full `/context` composition explorer.
- Budget policy editing.

## Dependencies

- AS-170, AS-025, AS-020, AS-063.
