---
id: AS-051
title: Headless CLI mode (scripting / CI)
status: ready-to-implement
github_issue: 51
depends_on: [AS-018, AS-031, AS-065]
area: faces
priority: P1
source: PRD.md §7.18, §5, §10 Q5; CLI-UX.md (D-CLI-3/4/7/8/9)
---

# AS-051 · Headless CLI mode

**Status: ready to implement**

## Description

The scripting/CI face (§7.18) and, per Q5's risk mitigation, the fallback programmatic surface while the ACP decision (AS-052) is pending. Also the substrate for "Async Ana" (§3) and the future runner (AS-054).

Builds on the CLI router/contract (AS-065); this ticket adds the headless
*behaviour* per [CLI-UX.md](../CLI-UX.md).

- `smith run "<prompt>"` runs one task non-interactively (prompt positional, via
  stdin, or `-f <file>` — no `-p` flag, D-CLI-3): no TUI, no prompts.
- Output modes (D-CLI-4): plain text (final answer; bare/no-ANSI on a non-TTY),
  `--output json` (structured result: answer, cost, session ID, stop reason),
  `--output stream-json` (event stream as it happens).
- Permission posture (D-CLI-9): headless defaults to allowlist-then-deny —
  anything that would ask is denied with a structured report (never hangs waiting
  for input); `--auto` opts into auto mode explicitly. Destructive context ops
  refuse without `--yes` (D-CLI-8).
- `--budget <$>` maps to AS-041 enforcement. **Exit codes:** extend AS-065's
  `0/1/2` additively with the distinct classes (permission-stop, budget-stop,
  cancellation, provider-error, internal-error) per UX.md §17.2 / D-CLI-7.
- Personality auto-off (per §7.21 — programmatic output stays clean), even before
  the theme layer exists: assert no decorative output on this path.
- Sessions created headlessly are normal sessions: resumable in the TUI, visible to `/insights`.

## Acceptance criteria

- [ ] A scripted run completes end-to-end in CI (keys via env vars) with parseable JSON output.
- [ ] A permission-requiring action in headless mode fails fast with a machine-readable reason.
- [ ] Budget stop produces the documented exit code and partial-result JSON.
- [ ] A headless session is `/resume`-able interactively afterward.

## Dependencies

- AS-065 (CLI router + arg/output/exit-code contract this extends), AS-018
  (face-agnostic loop), AS-031 (config); AS-041 (budgets) soft
