---
id: AS-053
title: The Matrix layer — personality theme + /serious kill switch
status: ready-to-implement
github_issue: null
depends_on: [AS-021, AS-022, AS-031]
area: polish
priority: P1
source: PRD.md §7.21, Appendix D, Appendix B
---

# AS-053 · Matrix personality layer + /serious

**Status: ready to implement**

## Description

§7.21 / Appendix D: the optional cosmetic theme — themed status lines ("entering the matrix…", "there is no spoon…") and role names (user = `Mr. Anderson`, router = `The Keymaker`, insights = `The Architect`, …). The engineering substance of this ticket is the **containment guarantee**, not the jokes.

- Personality config (Appendix D schema): `theme: matrix|none`, `serious_mode`, `intensity: full|subtle`, overridable name map — via AS-031.
- A single theming layer in the TUI chrome: spinners, status line, entity display-names, empty states. **Architecturally confined:** flavor strings live in one package; code paths that produce generated code, diffs, commits, file writes, error payloads, or programmatic output have no import path to it (enforceable by a lint/arch test).
- `/serious` toggles at runtime; `serious_mode: true` in config mutes globally. Defaults: on in interactive TUI, **off automatically** for headless/ACP/CI/async faces.
- Rotating themed status lines while working; plain equivalents when muted.

## Acceptance criteria (PRD §7.21 AC, verbatim where possible)

- [ ] Toggling serious mode removes all references with zero effect on behavior or output.
- [ ] No flavor text ever appears in code, commits, file writes, or programmatic responses — enforced by an architectural test (the flavor package is unimportable from those paths), not just review.
- [ ] Non-interactive faces default to clean output (asserted in AS-051's tests too).
- [ ] Name map and intensity overrides work per Appendix D.

## Dependencies

- AS-021 (TUI chrome), AS-022 (`/serious`), AS-031 (config)
