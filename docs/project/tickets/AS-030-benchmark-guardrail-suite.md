---
id: AS-030
title: Cost/speed benchmark suite (the D5 internal guardrail)
status: done
github_issue: 30
depends_on: [AS-018, AS-020]
area: quality
priority: P0
source: PRD.md D5, §6
---

# AS-030 · Cost/speed benchmark suite

**Status: ready to implement**

## Description

D5 makes "cheaper/faster than a *naive* harness on the same model" an **internal design criterion + guardrail metric, measured on a benchmark suite** — and §6 adds the guardrails that task success rate must not regress and `/clean`/`/tidy` must lose no data. None of that is measurable without this suite, so it should exist early, not after launch.

- Fixed suite of coding tasks run headlessly through Agent Smith with per-block token/cost accounting (AS-020).
- A "naive baseline harness" for comparison, same model, no context management.
- Metrics per run: cost per completed task, task success, time-to-first-token, median turn latency, end-of-session live-context %.
- Repeatable runner + report (markdown/JSON) comparing branches over time.

## Clarified implementation decisions

- **Task suite:** start with a small repo-local deterministic suite of 5-8 fixture tasks that can be judged by tests or file diffs, plus a harness shape that can later import public benchmark tasks. No external benchmark dependency is required for V1.
- **Naive baseline:** build and freeze a minimal headless loop using the same provider/model and tools but without Smith context projection, `/clean`, `/tidy`, routing, or cache-aware context trimming. Version the baseline alongside the suite.
- **Run economics/cadence:** the suite is not a per-PR CI gate by default. It supports dry-run/offline fixture validation locally and real provider runs on demand before release or major context/routing changes.
- **Variance:** each real-provider report records model/provider/settings and supports multiple repetitions, but V1 regression detection is report-only: flag large directional changes instead of failing CI on stochastic results.

## Acceptance criteria

- [x] One command runs the suite against a chosen provider/model and emits JSON plus Markdown reports. (`go run ./cmd/bench [-provider <name> -model <id>] -out <dir>`; `scripts/harness/benchmark.sh` for the offline run.)
- [x] Reports include all §6 primary + secondary metrics that exist at V1. (Per task: success, cost, tokens, time-to-first-token, median turn latency, end-of-session live-context %.)
- [x] Baseline harness is versioned and frozen alongside the suite. (`benchmark.NaiveHarness` — the same loop/provider/tools with no Smith context management, driven through the public `loop.WithProjector` seam.)
- [x] A deliberate context-bloat regression is detectable in the report. (`benchmark.Compare` flags cost/context growth past `RegressionThreshold`; the `context-bloat` fixture shows the naive baseline resending excluded context — `TestNaiveCostsMoreOnBloat`, `TestCompareDetectsContextBloatRegression`.)
- [x] Default tests for the benchmark framework are deterministic/offline. (A scripted provider drives the suite with no network; cost is a pure function of the fixtures.)

## Implementation notes

- `internal/benchmark` — `Runner` drives fixture `Task`s through `Harness`es (`SmithHarness`, frozen `NaiveHarness`) and prices the result with the embedded `cost.Table`. `Suite()` is the frozen offline fixture set (file-diff-judged tasks plus the `context-bloat` fixture). `Report` renders JSON + Markdown; `Compare` flags directional regressions.
- `cmd/bench` is the thin command; `scripts/harness/benchmark.sh` is the on-demand wrapper (documented as **not** a CI gate in [agent-quality-gates.md](../../agent-quality-gates.md)).
- Loop seam: added `loop.WithProjector` (additive option) so the naive baseline runs the *same* loop with a different context policy instead of forking the orchestrator.

## Dependencies

- AS-018 (a working loop to benchmark), AS-020 (accounting)
