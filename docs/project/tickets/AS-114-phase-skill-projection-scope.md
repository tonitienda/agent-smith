---
id: AS-114
title: Scope Coding Mode process-skill blocks to the active phase
status: ready-to-implement
github_issue: null
depends_on: [AS-074, AS-006]
area: coding-mode
priority: P2
source: AS-074 implementation follow-on
---

# AS-114 · Scope process-skill blocks to the active phase

**Status: ready-to-implement**

## Description

AS-074 auto-loads each phase's process skills as system text blocks on the log
(producer `coding-mode/skills`, tagged with the phase). They enter context when
the phase becomes active and **stay** in context for the rest of the session —
the analyse-phase grilling guidance is still in the window during implement and
verify. That is acceptable as a first cut (the guidance compounds), but it grows
the window and dilutes the *per-phase* intent.

This ticket scopes those blocks to the active phase in the projection engine
(AS-006): a process-skill block is live only while its tagged phase is the
current one, and drops out (like an exclusion/segment) once the mode moves on. It
stays on the log — auditable and reversible (D3) — but leaves the window when its
phase is no longer active.

## Acceptance criteria

- [ ] A `coding-mode/skills` block is in the live projection only while its
      tagged phase is the active Coding Mode phase.
- [ ] Re-entering the phase brings its guidance back without re-appending events.
- [ ] Exiting the mode drops all process-skill blocks from the window; history
      stays on the log.
- [ ] `/context` attributes the blocks to their skill while they are live.

## Dependencies

- AS-074 (the per-phase skill blocks this scopes), AS-006 (the projection engine
  that decides what is live).
