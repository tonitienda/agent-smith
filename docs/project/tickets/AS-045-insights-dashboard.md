---
id: AS-045
title: /insights — model-assisted session retrospective dashboard (flagship wedge)
status: ready-to-implement
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

- [ ] Every session can produce a dashboard; ≥1 suggestion is specific and applicable (PRD AC verbatim).
- [ ] Every suggestion cites measured evidence with a jump-to-transcript link.
- [ ] Applying a memory-file suggestion shows a diff and lands correctly.
- [ ] The retro runs on the cheap tier, async at session end, within its configured budget (§9 mitigation).
- [ ] Measured-signals section renders even with the model layer disabled (zero-cost mode).

## Dependencies

- AS-020 (cost data), AS-022 (panel), AS-044 (runs as a system sub-agent); AS-040 (goal anchoring) soft
