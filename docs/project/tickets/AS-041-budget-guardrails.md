---
id: AS-041
title: Budget guardrails + /budget command
status: done
github_issue: 41
depends_on: [AS-020, AS-022, AS-031]
area: cost
priority: P1
source: PRD.md §7.15, Appendix A
---

# AS-041 · Budget guardrails + /budget

**Status: done**

## Description

§7.15: per-session/per-task $ ceilings with warn + hard stop. No incumbent has this (§4 matrix) — it's a wedge-3 differentiator and a prerequisite for the async runner (AS-054) and sub-agent budget caps (Appendix C.3).

- `/budget <$>` sets the session ceiling; configurable defaults per project/user (AS-031).
- Warning at a configurable threshold (default 80%): visible banner + status-line state change.
- Hard stop at the ceiling: finish the in-flight tool call, then halt the turn with a clear message and options (raise budget / `/clean` / `/compact` / end). Never silently exceed.
- Per-task ceilings exposed as an API for subagents (AS-046) and system sub-agents (AS-044) to enforce.
- **Scoped out (explicitly):** the PRD's "budget mode that trims context aggressively" — that needs `/tidy`-grade trimming machinery; deferred to a follow-up ticket once AS-043 resolves.

## Acceptance criteria

- [x] A session with a $0.50 budget warns at $0.40 and halts at $0.50 (test with mock provider pricing). — `loop.TestBudgetWarnThenHalt`. Enforcement is boundary-based: it halts once recorded spend reaches the ceiling; see the limitation note below on single-turn overshoot and unpriced turns.
- [x] The hard stop is graceful: log consistent, session resumable after raising the budget. — the loop returns `StopBudget` at a turn boundary (in-flight tool calls already dispatched, no orphaned blocks); raising the ceiling appends a new `/budget` event and the next turn proceeds.
- [x] Budget state survives `/resume`. — the ceiling is an append-only `eventlog.KindBudget` event derived from the log on replay (`budget.Current`), not side state; `budget.TestSetCurrentRoundTrip` and `loop.TestBudgetOverrideFromLog`.
- [x] Sub-agent budget caps consume the same enforcement API. — `budget.Guard` is the stateless decision rule (spend → OK/Warn/Halt); the loop and any future sub-agent (AS-044/046) enforce a per-task cap through the same `Guard`.

## Implementation notes

- **`internal/budget`** owns the durable ceiling on the log (`Set`/`Current`, over `eventlog.KindBudget`) and the pure enforcement decision (`Guard.Check` → `OK`/`Warn`/`Halt`, inclusive ceiling: `spend == limit` halts). The zero `Guard` is disabled.
- **Loop** (`loop.WithBudget`): enforcement runs at each turn boundary, after the prior turn's usage is recorded and any tool call dispatched — emitting `UIBudgetWarning` once on first crossing and `UIBudgetHalt` + `StopBudget` once spend reaches the ceiling. Spend comes from the same `cost.Summarize` source as `/cost` and the meter, so the three never drift.
- **`/budget`** (inline, scriptable): `/budget` shows the ceiling, warn threshold, and spend; `/budget <amount>` sets it (tolerates a leading currency symbol); `/budget off` clears it (records a `0` ceiling).
- **Config defaults** (AS-031): `budget.limit_usd` (default ceiling for new sessions) and `budget.warn_fraction` (default 0.8); a `/budget` override on the log wins over both.
- **Status line**: the cost segment appends the ceiling (`$0.42/$0.50`) and colors by enforcement state (green/yellow/red); transcript notices use the pricing table's currency symbol, like `/cost`.
- **Known limitation (D0, documented not silent):** enforcement is boundary-based and only as accurate as priced spend. A turn's cost is known only after it completes, so a single turn can carry the total slightly past the ceiling before the next boundary halts the run; and a turn the pricing table cannot price contributes `$0`, so an unpriced/unknown model is effectively unmetered. Strict pre-turn non-overshoot (a reservation from a request-size + max-output estimate) and conservative handling of unpriced turns are spun out as **AS-086**.
- **Scoped out** (as specified): the "budget mode that trims context aggressively" — deferred behind AS-043 `/tidy`.

## Dependencies

- AS-020 (live cost), AS-022 (command), AS-031 (defaults)
