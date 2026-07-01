---
id: AS-170
title: Orchestrator daemon ships its own scheduler/concurrency; ADR D-ORCH-3 still says it "reuses the async runner"
status: needs-clarification
github_issue: null
depends_on: [AS-159, AS-161]
area: orchestrator
priority: P2
source: docs/architecture/orchestrator-architecture.md; docs/architecture/package-contracts.md; QA pass 2026-07-01
---

# AS-170 · Orchestrator ↔ async-runner reconciliation (ADR D-ORCH-3 vs shipped AS-161 daemon)

**Status: needs-clarification** *(raised during a QA pass comparing the
architecture docs, arch tests, and code; touches an Accepted ADR decision, so it
needs a human call before the docs are amended)*

## Description

The Accepted orchestrator ADR states, in **D-ORCH-3**
(`docs/architecture/orchestrator-architecture.md`):

> It **reuses the async runner (AS-054/AS-132)** for bounded concurrency rather
> than introducing a second execution path.

The shipped AS-161 daemon does **not** do this. It introduced its own
bounded-concurrency execution path inside the SQLite run store:

- `internal/orchestrator/daemon.go` owns the scheduler (cron entries, manual /
  GitHub enqueue), per-run leasing (`worker_id` + `heartbeat_at`), stale-heartbeat
  reclaim, per-key concurrency, retry/attempt accounting, and per-run timeout — a
  self-contained execution loop. The AS-161 ticket itself documents this design
  ("Bounded execution", "Run store split").
- `go list -f '{{.Imports}}' ./internal/orchestrator` → only
  `internal/orchestrator/spec`, `internal/orchestrator/store`, stdlib, and
  `gopkg.in/yaml.v3`. It does **not** import `internal/run` (the async runner) at
  all.
- The only `internal/run` touch is in the composition root
  (`cmd/smith/orchestrator.go`), and it is `run.DefaultRoot()` — a data-directory
  *path* constant, not the async runner's execution/queue.

So the shipped architecture is a **second execution path** — exactly what
D-ORCH-3 said it would avoid. This is defensible (D-ORCH-4 keeps run-control state
in the store; leasing there is cohesive and cgo-free), but the ADR decision text
and the derived `package-contracts.md` dependency row now describe a design the
code does not implement.

## Open questions

1. **Amend the ADR, or the code?** Either:
   - **(a) Update D-ORCH-3** to record the shipped decision — the daemon owns its
     own bounded concurrency via SQLite leases (D-ORCH-4), reusing `internal/run`
     only for the data-directory root — and note the "single execution path" goal
     was superseded because run-control state must live in the store; **or**
   - **(b) Refactor the daemon** to drive execution through the AS-054/AS-132 async
     runner as the ADR originally fixed, keeping only spec/policy in the store.

   (a) matches the shipped, tested, `done` implementation and is the likely answer;
   (b) is a larger change against working code. This ticket asks for the human
   sign-off to amend an Accepted ADR rather than making that call unilaterally in a
   QA pass.
2. **package-contracts.md row.** The orchestrator "Depends on" cell was corrected
   in the same `[QA]` PR to say the core-contract deps (incl. the async runner)
   arrive via the injected `Executor` seam and to cross-reference this ticket. If
   the answer to Q1 is (a), drop "async runner" from that list entirely, since the
   daemon will never depend on it. If (b), the row stands.
3. **Guard.** Whichever way it lands, consider whether a `layering_test.go` note
   should pin the orchestrator's relationship to `internal/run` so the ADR and the
   imports cannot drift again.

## Notes

- No functional bug: the daemon works and its tests pass. This is a
  documentation-vs-decision-record integrity issue on an Accepted ADR.
- Related doc drift found in the same pass (tool/routing/redaction dependency
  claims) was corrected directly; this one is escalated because it changes an ADR
  decision, not just a descriptive sentence.
