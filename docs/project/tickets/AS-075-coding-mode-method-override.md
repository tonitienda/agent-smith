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

- [x] A project memory file can reorder/skip phases and add a phase rule; the
      phase tracker (AS-073) and shell (AS-072) reflect the customised method.
- [x] With no override present, the default house method is used unchanged.
- [x] Override resolution follows project memory > skill pack > baked-in default.
- [x] Malformed/partial customisation degrades to the default for the unspecified
      parts rather than failing the mode (tolerant, additive-only).

## Dependencies

- AS-032 (memory files merge — where overrides are read from), AS-072 (the phase
  model being overridden).

## Implementation notes (done)

The override is a declarative fenced block in any memory file (`CLAUDE.md` /
`AGENTS.md` / `AGENT.md`), so it is read the same way other project config is —
no code execution (security posture):

```smith-method
phases: think, plan, implement, verify   # reorder / skip in one
skip: refactor                           # drop a phase without re-listing
rule: require a ticket before any code   # repeatable; surfaced in the panel
```

- `internal/mode/method.go`: `ParseOverride` + `ResolveMethod(default, memos)`
  layer the overrides over `DefaultPhases`, most-specific (last) memo wins for the
  phase order; `skip` and `rule` accumulate. The lifecycle core stays string-only
  (it never imports `memory` or skill content) — the composition root passes in the
  memory block texts.
- `cmd/smith/controller.go`: `resolveMethod(events)` collects the on-log memory
  blocks (already appended in precedence order by `seedMemory`) and drives entry,
  `/mode`, `/phase`, the tracker, and the `mode.Panel` from the resolved method, so
  the customised method is what the shell and tracker reflect (D3, log-derived).
- Per-phase **stance** override is already handled by AS-074's same-name
  skill-shadowing (`resolvePhaseSkill`); this ticket adds the phase-order and rule
  layers on top.
- Tolerant/additive (D2): an unrecognised or malformed directive is ignored and
  the unspecified parts fall back to the default, so a partial override never fails
  the mode.
