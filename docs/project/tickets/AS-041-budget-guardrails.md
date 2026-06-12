---
id: AS-041
title: Budget guardrails + /budget command
status: ready-to-implement
github_issue: 41
depends_on: [AS-020, AS-022, AS-031]
area: cost
priority: P1
source: PRD.md §7.15, Appendix A
---

# AS-041 · Budget guardrails + /budget

**Status: ready to implement**

## Description

§7.15: per-session/per-task $ ceilings with warn + hard stop. No incumbent has this (§4 matrix) — it's a wedge-3 differentiator and a prerequisite for the async runner (AS-054) and sub-agent budget caps (Appendix C.3).

- `/budget <$>` sets the session ceiling; configurable defaults per project/user (AS-031).
- Warning at a configurable threshold (default 80%): visible banner + status-line state change.
- Hard stop at the ceiling: finish the in-flight tool call, then halt the turn with a clear message and options (raise budget / `/clean` / `/compact` / end). Never silently exceed.
- Per-task ceilings exposed as an API for subagents (AS-046) and system sub-agents (AS-044) to enforce.
- **Scoped out (explicitly):** the PRD's "budget mode that trims context aggressively" — that needs `/tidy`-grade trimming machinery; deferred to a follow-up ticket once AS-043 resolves.

## Acceptance criteria

- [ ] A session with a $0.50 budget warns at $0.40 and halts before exceeding $0.50 (test with mock provider pricing).
- [ ] The hard stop is graceful: log consistent, session resumable after raising the budget.
- [ ] Budget state survives `/resume`.
- [ ] Sub-agent budget caps consume the same enforcement API.

## Dependencies

- AS-020 (live cost), AS-022 (command), AS-031 (defaults)
