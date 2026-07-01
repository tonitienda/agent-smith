---
id: AS-151
title: Smith event-log integration for orchestrated runs
status: done
area: observability
priority: P2
depends_on: [AS-161]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-151 · Smith event-log integration for orchestrated runs

## Description

Ensure every orchestrated job/run is persisted as a normal Smith append-only session with additional metadata for schedule, trigger, provider role, GitHub refs, PR links, artifacts, policy decisions, and cost.

## Acceptance criteria

- [x] Orchestrated runs create readable/resumable Smith sessions rather than a separate log format.
- [x] Session metadata links job ID, trigger ID, run DB ID, attempt number, provider role, GitHub refs, PR links, and artifact IDs.
- [x] GitHub actions, policy checks, provider calls, costs, and terminal outcomes are represented as event-log blocks or referenced metadata.
- [x] Cost and insights readers can process orchestrated sessions without a separate code path.
- [x] Large artifacts are integrity-checked and referenced rather than embedded in the JSONL event log.

## Implementation

The session seam lives in `internal/orchestrator`:

- **`runlog.go`** — a `Recorder` creates one ordinary Smith session per run
  (`session.Store.CreateWith`), stamps the run linkage (`RunLink`: job/run/trigger
  /attempt + repo/org/owner, growing PR links and artifact ids) onto the session
  metadata under `Metadata.Ext["orchestration"]` so operators can list runs
  without replaying the log, and appends lifecycle blocks. The five orchestration
  kinds (`orchestration_run_start`/`_policy`/`_github`/`_artifact`/`_run_outcome`)
  are non-content harness kinds whose payload rides on `Block.Ext` (D2 additive
  escape hatch, like `KindEscalation`); the frozen union (AS-003) is untouched.
  Provider spend is recorded as the standard `eventlog.KindUsage` block, so
  `cost.Summarize` / `insights.Analyze` price an orchestrated session with no
  separate code path. Artifacts are rejected unless they carry a `uri` + `sha256`
  (integrity-checked reference, never embedded bytes).
- **`sessionexec.go`** — `SessionExecutor` decorates an inner `Executor`
  (default `StubExecutor`), opening a `Recorder`, running the real work, and
  writing the terminal outcome back into the run's session; `Outcome.SessionID`
  points at the recorded session so the run store links to it. AS-149/150 step
  executors record their own GitHub/policy/usage blocks against the same run via
  the exported `Recorder`.
- **`internal/session`** — additive `Metadata.Ext` field + `CreateWith` /
  exported `WriteMetadata` so identity metadata round-trips through `metadata.json`.
- Wired into `smith runs daemon start` (`cmd/smith/orchestrator.go`).

Decoders (`PolicyDecisionOf`, `GitHubActionOf`, `ArtifactRefOf`, `RunOutcomeOf`,
`RunLinkOf`) are exported for the operator surface (AS-155).

## Clarification (resolved 2026-06-30)

This ticket carried no ticket-local open question; it was held at
`needs-clarification` pending the [orchestrator ADR](../../architecture/orchestrator-architecture.md)
(AS-159), now **Accepted**, and AS-161 (daemon/scheduler/SQLite run store), now
**done**. The ADR's D-ORCH-4 boundary table assigns this exact seam: "Smith
session / event log | core (AS-005/006/007) | Each run is a normal append-only
Smith session; `/context`, `/cost`, `/insights`, replay reuse existing readers
(AS-151). No second observability path." AS-161's own status note confirms the
split it builds on: "narrative/cost stays in the session log" while the SQLite
run store holds only run-control state (jobs/triggers/runs/leases/attempts/
idempotency/audit). So the design is fixed — extend the existing append-only
session/event-log readers with the metadata this ticket's acceptance criteria
list, rather than introducing a parallel log format.

## Dependencies

[AS-161]
