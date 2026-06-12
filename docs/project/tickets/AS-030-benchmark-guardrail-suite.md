---
id: AS-030
title: Cost/speed benchmark suite (the D5 internal guardrail)
status: needs-clarification
github_issue: 30
depends_on: [AS-018, AS-020]
area: quality
priority: P0
source: PRD.md D5, §6
---

# AS-030 · Cost/speed benchmark suite

**Status: needs clarification**

## Description

D5 makes "cheaper/faster than a *naive* harness on the same model" an **internal design criterion + guardrail metric, measured on a benchmark suite** — and §6 adds the guardrails that task success rate must not regress and `/clean`/`/tidy` must lose no data. None of that is measurable without this suite, so it should exist early, not after launch.

- Fixed suite of coding tasks run headlessly through Agent Smith with per-block token/cost accounting (AS-020).
- A "naive baseline harness" for comparison, same model, no context management.
- Metrics per run: cost per completed task, task success, time-to-first-token, median turn latency, end-of-session live-context %.
- Repeatable runner + report (markdown/JSON) comparing branches over time.

## Open questions (why this needs clarification)

1. **Task suite definition** — which tasks, how many, which repos? Hand-built micro-tasks, an adapted public benchmark (e.g., SWE-bench-style subset), or both? Who judges "completed"?
2. **What exactly is the "naive baseline harness"?** A minimal loop we build and freeze? Pinning this down is required for the comparison to mean anything.
3. **Run economics** — real API spend per benchmark run; budget and cadence (per-PR is too expensive — nightly? pre-release?).
4. **Variance handling** — models are stochastic (§8 rules out full determinism); how many repetitions per task, and what counts as a regression?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] One command runs the full suite against a chosen provider/model and emits a comparable report.
- [ ] Reports include all §6 primary + secondary metrics that exist at V1.
- [ ] Baseline harness is versioned and frozen alongside the suite.
- [ ] A deliberate context-bloat regression is detectable in the report (prove the guardrail works).

## Dependencies

- AS-018 (a working loop to benchmark), AS-020 (accounting)
