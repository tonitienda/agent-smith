---
id: AS-051
title: Headless CLI mode (scripting / CI)
status: ready-to-implement
github_issue: 51
depends_on: [AS-018, AS-031]
area: faces
priority: P1
source: PRD.md §7.18, §5, §10 Q5
---

# AS-051 · Headless CLI mode

**Status: ready to implement**

## Description

The scripting/CI face (§7.18) and, per Q5's risk mitigation, the fallback programmatic surface while the ACP decision (AS-052) is pending. Also the substrate for "Async Ana" (§3) and the future runner (AS-054).

- `smith -p "<prompt>"` runs one task non-interactively: no TUI, no prompts.
- Output modes: plain text (final answer), `--output json` (structured result: answer, cost, session ID, stop reason), `--output stream-json` (event stream as it happens).
- Permission posture: headless defaults to allowlist-only — anything that would ask is denied with a structured report (never hangs waiting for input); `--auto` opts into auto mode explicitly.
- `--budget <$>` maps to AS-041 enforcement; nonzero exit codes distinguish task failure / budget stop / permission stop.
- Personality auto-off (per §7.21 — programmatic output stays clean), even before the theme layer exists: assert no decorative output on this path.
- Sessions created headlessly are normal sessions: resumable in the TUI, visible to `/insights`.

## Acceptance criteria

- [ ] A scripted run completes end-to-end in CI (keys via env vars) with parseable JSON output.
- [ ] A permission-requiring action in headless mode fails fast with a machine-readable reason.
- [ ] Budget stop produces the documented exit code and partial-result JSON.
- [ ] A headless session is `/resume`-able interactively afterward.

## Dependencies

- AS-018 (face-agnostic loop), AS-031 (config); AS-041 (budgets) soft
