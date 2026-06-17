---
id: AS-074
title: Coding Mode process skill pack (bundled, auto-enabled per phase)
status: ready-to-implement
github_issue: 124
depends_on: [AS-034, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-5.2, -6, -8)
---

# AS-074 · Coding Mode process skill pack

**Status: ready-to-implement**

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
