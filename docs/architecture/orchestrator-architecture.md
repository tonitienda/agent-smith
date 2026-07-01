# Orchestrator architecture and product boundaries (ADR)

> Status: **Accepted** · Ticket: AS-159 · Source PRD:
> [smith-orchestrator-dogfood-prd.md](../project/smith-orchestrator-dogfood-prd.md)

This ADR records the architecture decision for the Smith orchestrator — the
always-on, deterministic workflow engine that lets **Smith implement Smith**. It
fixes the boundaries, the deployment modes, the non-goals, and the open-question
triage that the rest of the orchestrator wave (AS-160, AS-161, AS-147 … AS-158)
builds on. Where this ADR and the draft PRD disagree, this ADR wins.

## Context

The maintainer workflow for building Agent Smith is currently scattered across
external schedulers and vendor sessions (GitHub, Anthropic, OpenAI, manual
hand-offs). Smith already has the inward-core substrate this needs — an
append-only event log (AS-005), context projection (AS-006), session persistence
(AS-007), provider routing (AS-042/AS-110), cost accounting (AS-020), a local
async runner and background daemon (AS-054/AS-132), and delegated tasks across
faces (AS-119/AS-120). The missing layer is an **orchestrator** that owns
schedules, GitHub triggers, workflow state, deterministic GitHub actions, policy
gates, budgets, and run history in one place, and delegates only the *cognitive*
work to models.

## Decision

### D-ORCH-1 — Smith owns the deterministic shell; models do bounded cognition

The orchestrator is a **deterministic workflow engine**, not an agent. Smith owns
workflow state, schedules, GitHub event handling and actions, permissions, secret
scopes, budgets, labels, retries, and merge policy. Models write code, review,
test, and reason inside steps with declared budgets and roles. Every meaningful
decision is recorded in the run store or the Smith event log. Underspecified or
unsafe states **fail closed**.

### D-ORCH-2 — One binary, noun-grouped command: `smith runs daemon`

The orchestrator ships inside the existing `smith` binary under the `runs` noun
group rather than as a separate `smithd` binary (PRD Open Q1). This keeps the
single static-binary build (`make build`) and the single composition root
(`cmd/smith` → `internal/smithapp`, per the package-contracts doc and AS-089).
`smithd` may later be added as a thin alias/symlink that execs `smith runs
daemon`, but it is not a second entry point.

Surfaces:

- `smith runs daemon` — start the long-lived orchestrator process.
- `smith runs {list,inspect,rerun,cancel,pause,resume,health}` — operator
  control over jobs and runs (CLI-first; the operator API/UI in AS-155 wraps the
  same verbs, it does not replace them).

> **AS-161 implementation refinement.** `runs` already hosts the AS-054 async
> prompt-run queue (`list/status/work/resume`) — a different notion of "run". To
> avoid overloading those verbs, AS-161 nests the orchestrator one level deeper:
> the process is `smith runs daemon start` and the operator verbs are
> `smith runs daemon {list,inspect,rerun,cancel,pause,resume,health}`. This keeps
> the `smith runs daemon` entry this ADR fixed while disambiguating the operator
> surface from the prompt-run queue.

### D-ORCH-3 — New `internal/orchestrator` boundary at the orchestration tier

The orchestrator lives behind a new `internal/orchestrator` package at the
**orchestration layer** — the same tier as `internal/loop`, alongside the faces
(PRD Open Q2). It depends inward on core contracts (event log, provider, config,
cost, the async runner) and is **never imported by inward-core packages**. This
is enforced by `internal/archtest` exactly like the existing loop/face/core
rules (AS-098, AS-145/AS-146 archtest guards, AS-141, AS-142). It reuses the
async runner (AS-054/AS-132) for bounded concurrency rather than introducing a
second execution path.

### D-ORCH-4 — Documented internal boundaries

| Seam | Owner | Responsibility |
| --- | --- | --- |
| Daemon / scheduler | `internal/orchestrator` (AS-161) | Long-lived process; load+validate specs; cron/manual/GitHub enqueue; bounded concurrency; lifecycle verbs. |
| Run store | `internal/orchestrator` SQLite (AS-161) | Jobs, triggers, queued runs, leases, attempts, terminal state, idempotency keys, audit entries. **Run-control state only** — narrative/cost stays in the session log. |
| Smith session / event log | core (AS-005/006/007) | Each run is a normal append-only Smith session; `/context`, `/cost`, `/insights`, replay reuse existing readers (AS-151). No second observability path. |
| Job spec / DSL | `.agent-smith/jobs/*.yaml` (AS-160, format in [job-spec-dsl.md](../design/job-spec-dsl.md)) | Repo-reviewed, versioned, declarative. Steps/hooks/actions/routing/policy declared, never prompt-controlled. |
| GitHub integration | `internal/orchestrator` (AS-147/149) | Normalize webhooks → trigger events; deterministic action steps (labels, PR create/update, comment, status, guarded merge). |
| Provider routing | core routing (AS-042/110) via per-step policy (AS-150) | Role↔provider separation; explicit per step or a named routing policy. |
| Secrets | secret contract (AS-154, [ADR-0004](../design/adr-0004-secret-management-redaction.md); leaf `internal/orchestrator/secret`) | Declared scopes classified into 4 classes; resolved through a proxy seam into a non-rendering `Value`; injection audit record carries name/scope/expiry/recipient/run — never a value; value-based redaction-at-capture plus the AS-115 pattern scrub; fail closed on missing/unknown scope. |
| Sandbox seam | sandbox interface (AS-153) | Local checkout first; rootless container / microVM behind one interface later. |
| Operator API/UI | AS-155 | Thin wrapper over the same run-control verbs; not a control path of its own. |

### D-ORCH-5 — Deployment modes are separated, seams designed in from day one

1. **Local daemon** (MVP 0) — `smith runs daemon` on the maintainer machine; local checkout; SQLite run store.
2. **Private VPC daemon** (MVP 1) — same binary on a private host; durable webhook delivery, health checks, backups, runbooks; single-tenant.
3. **Remote workers / sandboxes** (MVP 2) — worker claim/heartbeat/stream/finish; rootless-container or microVM backend behind the AS-153 sandbox interface; short-lived credentials, egress policy, teardown.
4. **Hosted execution** (MVP 3, future) — GitHub App onboarding, operator UI/API, multi-tenant boundaries — only after dogfood stabilizes, and never in conflict with PRD D9 ("not a sandbox" — see [hosted-agent-sandboxing.md](../design/hosted-agent-sandboxing.md)).

### D-ORCH-6 — Non-goals (fail-closed)

Explicitly **out of scope**, and the engine must refuse them rather than degrade:

- Smith editing its own job specs.
- Jobs creating other jobs.
- Prompts deciding labels, permissions, retries, merges, or workflow-state transitions (these are deterministic steps/policy only).
- Bypassing branch protection or repository review policies; force-push; merging on failed or unknown checks.
- General-purpose untrusted-compute hosting; long-lived mutable dev machines; plugin marketplace / arbitrary third-party plugins; model training.

## Open-question triage (PRD §6 + ticket open questions)

| # | Question | Resolution |
| --- | --- | --- |
| Q1 | Daemon command shape | **Decided:** `smith runs daemon` (D-ORCH-2); `smithd` only as a later alias. |
| Q2 | Existing packages vs new boundary | **Decided:** new `internal/orchestrator`, orchestration tier, archtest-guarded (D-ORCH-3). |
| Q3 | Minimum SQLite run store vs session log | **Decided (split):** run store holds run-control state (jobs/triggers/runs/leases/attempts/idempotency/audit); narrative + cost stay in the session log (D-ORCH-4). Exact schema → **AS-161**. |
| Q4 | Specs repo-only or UI-editable | **Decided:** repo-only and repo-reviewed for the dogfood wave; the future UI may *propose* and export to repo but the repo stays the source of truth. Reinforces non-goal "Smith editing its own job specs." → **AS-160**. |
| Q5 | Auto-merge policy for Smith PRs | **Deferred to AS-157**, constrained by D-ORCH-6 (author-is-Smith + required-checks-green + required labels; never bypass protection / merge on unknown checks). |
| Q6 | Routing explicit per step vs delegated | **Decided:** both supported — explicit per-step provider policy or a named routing policy/skill (D-ORCH-4). Mechanism → **AS-150**. |
| Q7 | First secrets approach | **Decided (AS-154):** declared scopes in 4 classes, credential-proxy resolver seam so values never enter spec/DB/log, non-rendering `Value`, injection audit record without values, value-based redaction-at-capture (complementing AS-115), fail-closed on missing/unknown scope. Full record: [adr-0004-secret-management-redaction.md](../design/adr-0004-secret-management-redaction.md). |
| Q8 | First sandbox backend | **Decided for MVP 0:** none / local checkout. Container/microVM behind the AS-153 interface, informed by **AS-158**. |
| Q9 | First budget ceilings | **Deferred** to the dogfood workflow pack (**AS-152**); enforced via existing budget guardrails (AS-041/AS-086) per step and per run. |
| Q-148 | GitHub auth: App vs scoped token | **Decided (AS-148):** MVP 0 = tightly scoped fine-grained maintainer PAT (Contents/PRs/Issues r/w, Checks read); GitHub App minting short-lived per-operation installation tokens is the MVP 1+ migration target behind a credential-accessor seam. Full record: [adr-0003-github-auth-strategy.md](../design/adr-0003-github-auth-strategy.md). |

## Downstream ticket disposition

Following AS-159, the architecture/boundary questions are resolved, so the two
foundational build tickets move to **ready-to-implement**:

- **AS-160** — job spec / DSL: location, shape, and declarative-action principle fixed here; the format is now specified in [job-spec-dsl.md](../design/job-spec-dsl.md) (status: done).
- **AS-161** — daemon / scheduler / SQLite run store: command shape, package boundary, and store/session split fixed here. **Status: done** — `internal/orchestrator` (+ `/store`) ships the loader, SQLite run store, cron/manual/GitHub scheduler, bounded execution, and the `smith runs daemon` operator surface; the real step `Executor` is wired by AS-147/149/150 (session persistence landed in AS-151 — `SessionExecutor` records each run as a normal Smith session).
- **AS-147** — GitHub event ingestion and deterministic hooks: **done**. `Normalize` (webhook.go) maps issue/PR/comment deliveries to trigger records, and each GitHub-triggered run persists its targetable context (repository, issue/PR number, actor) in the run store's additive `runs.trigger_context` column. The deterministic label/comment/status hooks a job declares run through the narrow `GitHubActions` port (hooks.go), driven by `SessionExecutor` at the `on_start`/`on_success`/`on_failure`/`on_cancel` lifecycle points and recorded on the run's session as `orchestration_github` blocks. The authenticated transport implementing that port is AS-148; the PR-lifecycle actions (branch/PR/merge) are AS-149.

These stay **needs-clarification**, each gated on a product decision and/or the
AS-158 research spike (their ticket Open-questions sections name the blocker):
AS-147, AS-148, AS-149, AS-150, AS-151, AS-152, AS-153, AS-154, AS-155, AS-156,
AS-157.

- **AS-158** — competitive workflow / sandbox / secrets research spike: **done** ([research notes](../research/orchestrator-competitive-research.md)); it feeds AS-148, AS-153, AS-154, AS-156, AS-157.

## Consequences

- A clear, archtest-guarded place for orchestration code (`internal/orchestrator`) that cannot leak into inward-core.
- One binary, one composition root, one observability path (the session log) — no second analytics or execution stack.
- Product safety is structural: deterministic shell + fail-closed non-goals, not prompt discipline.
- The hosted path stays explicitly future and bounded by D9, so the dogfood wave does not accidentally build a multi-tenant sandbox.
