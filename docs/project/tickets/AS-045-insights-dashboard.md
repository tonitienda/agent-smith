---
id: AS-045
title: /insights — model-assisted session retrospective dashboard (flagship wedge)
status: done
github_issue: 45
depends_on: [AS-020, AS-022, AS-044]
area: insights-wedge
priority: P1
source: PRD.md §7.14, §9, D6 (fast-follow)
---

# AS-045 · /insights session dashboard

**Status: ready to implement**

## Description

The fourth flagship wedge (§7.14): a session retrospective nobody else ships (§4 matrix). Implemented as the `insights-writer` system sub-agent (schedule: `session_end`, cheap tier, async — per Appendix C.3) plus a `/insights` panel.

- **Measured signals first** (no model needed, computed from the log): cost breakdown per turn; slowest/most expensive turns; repeated file reads; tool-output tokens never referenced again (the "40k unused tokens" case); error/retry loops; context-health timeline (window growth, live-vs-stale ratio over time).
- **Model-assisted layer** (cheap tier): turns measured signals into **concrete suggestions** — "add `make test` to AGENT.md" (it was typed 4×), "scope this MCP server", "this span was a dead end — `/clean` it next time." §9 mitigation: every suggestion must cite its measured evidence (turns, tokens, counts) — never vibes.
- **One-click apply where safe:** memory-file edits go through diff preview (AS-024); anything else is copy-able text.
- Anchored to `/goal` (AS-040) when set: did the session meet its objective?
- Opt-in + cost-bounded per the C.3 config; `/insights` on a session with the writer disabled offers to run it on demand.

## Acceptance criteria (PRD §7.14 AC included)

- [x] Every session can produce a dashboard; ≥1 suggestion is specific and applicable (PRD AC verbatim).
- [x] Every suggestion cites measured evidence with a jump-to-transcript link (`#<seq>` anchors).
- [x] Applying a memory-file suggestion shows a diff and lands correctly (`/insights apply <n>`).
- [x] The retro runs on the cheap tier, async at session end, within its configured budget (§9 mitigation) — the insights-writer is a session-end sub-agent that makes **no** model calls, so it is within budget by construction.
- [x] Measured-signals section renders even with the model layer disabled (zero-cost mode) — there is no model layer in this slice; the whole dashboard is measured-first.

## Implementation notes

- `internal/insights`: `Analyze(events, table, model) Report` computes the measured signals (cost per turn, costliest turns, repeated commands/reads, oversized tool outputs, error loops, live-vs-stale context health) and grounded suggestions; `Render(Report)` is the face-agnostic dashboard. The package also houses the **insights-writer** system sub-agent (`writer.go`), registered in `cmd/smith/buildSubAgents`.
- `/insights` (and `smith insights`) is wired in the shared command registry; `/insights apply <n>` lands a suggestion's propose-only memory edit through a shown diff (deterministic numbering, no staged-preview state needed).
- **Scope cut (documented per D0):** the **model-assisted rewrite layer** (turning measured signals into richer model-authored prose) is deferred to **AS-109**. The deterministic, rule-based suggestions already satisfy the AC of ≥1 specific, applicable suggestion, and keeping the writer model-free makes the cheap-tier/budget AC trivially true.
- Goal anchoring (AS-040, "did the session meet its objective?") is a soft dependency and is **not** wired here; folded into AS-109.

## Dependencies

- AS-020 (cost data), AS-022 (panel), AS-044 (runs as a system sub-agent); AS-040 (goal anchoring) soft
