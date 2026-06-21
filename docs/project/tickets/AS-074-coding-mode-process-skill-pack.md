---
id: AS-074
title: Coding Mode process skill pack (bundled, auto-enabled per phase)
status: done
github_issue: 124
depends_on: [AS-034, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-5.2, -6, -8)
---

# AS-074 · Coding Mode process skill pack

**Status: done**

## Implementation notes

- Bundled pack lives in `internal/codingskills` (`//go:embed skills`), parsed
  through `skill.LoadFS` so a bundled skill is an ordinary `skill.Skill` — five
  skills: `grill-gaps`, `find-side-effects` (analyse), `plan-review` (plan),
  `verify-checklist` (verify), `reflect-notes` (reflect).
- The phase→skill mapping is data on the phase definitions
  (`mode.PhaseSkills`), keeping the lifecycle core string-only (no skill-content
  dependency).
- Auto-load: on each Coding Mode phase entry the controller appends the phase's
  skill bodies as system text blocks (producer `coding-mode/skills`, attributed
  to the skill, tagged with the phase), deduped per `(instance, phase, skill)`.
  With the mode off, nothing is injected (zero cost). A project/user skill of the
  same name shadows the bundled one; an empty-body override disables it (AS-075).
- Grounding (D-CODE-8) is enforced as a machine-checkable predicate
  `codingskills.IsGrounded`; tests assert each bundled skill demonstrates a
  grounded finding and rejects generic advice.
- **Follow-on (filed):** AS-114 — scope process-skill blocks to the active phase
  in the projection so they don't persist in context after the phase ends.

## Description

The phase behaviors that make the advisor *opinionated* ship as **bundled
skills** (D-CODE-5.2), auto-invoked per phase via the existing skills model
(AS-034) — the user never installs or pulls them (D-CODE-6).

- A skill pack shipped in-tree: e.g. `grill-gaps` (analyse), `find-side-effects`
  (analyse), `plan-review` (plan), `verify-checklist` (verify), and a reflect
  helper. The phase definitions (AS-072) declare which skills belong to which
  phase; Smith loads them itself when the phase is active.
- **Grounded, not preachy (D-CODE-8):** these skills must cite the concrete
  thing — file, function, missing test, ticket — never "consider best
  practices." Same evidence discipline `/insights` demands.
- Skills are individually swappable/improvable (they are ordinary skills), so a
  project can disable or replace one without touching the mode core.

## Acceptance criteria

- [ ] The process skills ship with the binary; Coding Mode works with no install
      step (D-CODE-6).
- [ ] Each phase auto-invokes its declared skills via the AS-034 mechanism; with
      Coding Mode off, the skills add zero cost.
- [ ] Grilling/gap/side-effect output references concrete code/tickets, not
      generic advice (a test asserts findings carry a file/symbol/span reference).
- [ ] A skill can be disabled or replaced per project without breaking the mode
      shell.

## Dependencies

- AS-034 (portable skills loading + auto-invoke), AS-072 (phase definitions
  declare the per-phase skill set).
