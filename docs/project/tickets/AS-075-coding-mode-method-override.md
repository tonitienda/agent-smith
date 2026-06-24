---
id: AS-075
title: Coding Mode project-level method override (via memory files)
status: done
github_issue: 125
depends_on: [AS-032, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-5.3)
---

# AS-075 · Coding Mode project-level method override

**Status: ready-to-implement**

## Description

The opinionated method is layered, most-specific wins (D-CODE-5): baked-in house
method → process skill pack (AS-074) → **project memory**. This ticket is the
third layer: let a project customise the process via its memory files
(`CLAUDE.md` / `AGENTS.md` / `AGENT.md`, AS-032).

- A project can override or extend the method: reorder phases, skip a phase
  ("this repo skips refactor"), add a project rule ("require a ticket before any
  code"), or adjust the per-phase stance.
- Resolution order: project memory overrides the skill pack and the baked-in
  default; absent any override, the default house method applies unchanged.
- Customisation is **declarative** and read from memory the same way other
  project config is — no code execution, consistent with the security posture.

## Acceptance criteria

- [ ] A project memory file can reorder/skip phases and add a phase rule; the
      phase tracker (AS-073) and shell (AS-072) reflect the customised method.
- [ ] With no override present, the default house method is used unchanged.
- [ ] Override resolution follows project memory > skill pack > baked-in default.
- [ ] Malformed/partial customisation degrades to the default for the unspecified
      parts rather than failing the mode (tolerant, additive-only).

## Dependencies

- AS-032 (memory files merge — where overrides are read from), AS-072 (the phase
  model being overridden).
