# Smith Cloud dogfood PRD — always-on orchestrated agent work

> Status: **Draft v0.2** · Owner: Toni · Date: 2026-06-25
>
> Goal: make Smith capable of replacing the maintainer's external Assembly Line / Claude Code Routines workflow for implementing Agent Smith itself, while preserving Smith's provider-neutral event-log substrate and making always-on execution an explicit, deterministic, audited product surface.

## 1. Problem and target outcome

Agent Smith already has a local async runner and delegated tasks, but the current maintainer workflow for building Smith is scattered across several external systems: GitHub schedules/actions, Anthropic sessions, OpenAI/GPT review sessions, and manual coordination. The missing layer is not "cloud" first. The missing layer is an **always-on Smith orchestrator** that owns schedules, triggers, workflow state, provider routing, GitHub actions, policy gates, event logs, and run history in one place.

The immediate dogfood objective is to replace the maintainer's external Assembly Line / Claude Code Routines setup for Smith's own development:

- configure recurring, manual, and GitHub-triggered Smith jobs in one place;
- separate workflow scheduling from model/provider selection, so Anthropic, OpenAI/GPT, Gemini, Grok, or compatible providers can be assigned per role/step;
- run deterministic workflows such as implementation, review, manual-test, architecture-check, and documentation-drift passes;
- react to GitHub events such as PR merged, issue labeled, PR labeled, or comment command;
- perform deterministic GitHub actions such as adding labels, creating/updating PRs, commenting summaries, setting statuses, and enabling/performing policy-gated auto-merge;
- fail closed and clearly when required permissions, labels, secrets, checks, or budgets are missing;
- eventually run from an always-on private VPC/Hetzner deployment rather than requiring the maintainer's laptop to stay open 24/7; and
- store every run as a normal Smith append-only session so the local product wedges (`/context`, `/cost`, `/insights`, replay, compliance archive) remain useful.

This dogfood path can later grow into a hosted execution product for users, companies, and enterprises, but the first milestone is private, single-repo, and opinionated around Smith implementing Smith.

## 2. Users and non-goals

### Primary users

- **Maintainer dogfooder:** wants Smith to work on Smith itself without relying on Claude Code Routines or a bespoke Assembly Line.
- **Reviewer/operator:** reviews generated PRs, sandbox logs, run costs, provider outputs, and approval/merge decisions.
- **Trusted team admin:** later connects a GitHub organization, model-provider accounts, and repository policies to scheduled autonomous work.

### Non-goals for this phase

- General-purpose untrusted compute hosting.
- Long-lived mutable developer machines.
- A plugin marketplace or arbitrary third-party code plugins.
- Training/fine-tuning models.
- Smith editing its own job specs.
- Jobs creating other jobs.
- Letting prompts decide labels, permissions, retries, merges, or workflow state transitions.
- Bypassing repository branch protections or human/repository review policies.

## 3. Product principles

1. **Smith owns orchestration.** Models implement, review, test, and reason; Smith owns schedules, triggers, labels, retries, permissions, budgets, PR actions, and merge decisions.
2. **Cloud runs are still Smith sessions.** The event log, block schema, cost accounting, provider normalization, insights, and replay contracts are reused rather than forked.
3. **Provider-neutral scheduling.** A job chooses model/provider through explicit routing policy or role/subagent configuration; Anthropic/OpenAI-specific concepts do not leak into the schedule spec except as optional provider config.
4. **Determinism beats prompt magic.** Workflow actions such as `github.add_label`, `github.create_pr`, `github.enable_auto_merge`, and `github.merge_pr` are declarative steps/hooks, not instructions hidden inside a prompt.
5. **Least privilege by construction.** GitHub repository access, model-provider credentials, network egress, and user/team secrets are scoped to the job/subtask and injected just-in-time.
6. **Policy beats automation.** Auto-PR, auto-update, and auto-merge are allowed only when repository policy, branch protections, required checks, and explicit job policy allow them.
7. **Auditable dogfood first.** The first always-on deployment can be private and narrow, but its security model must be the one future users can understand.
8. **Cloud is a deployment mode.** The same orchestrator should be able to run locally, as a daemon on a private VPC host, and later behind a hosted control plane.

## 4. Core capabilities

### 4.1 Job spec, schedule, and trigger model

Smith supports a versioned job spec stored in-repo under `.agent-smith/jobs/*.yaml` and optionally imported into a control plane/UI later.

Triggers:

- cron-like schedules with timezone and missed-run policy;
- GitHub events, especially PR merged, issue labeled, PR labeled, comment command, and scheduled repository maintenance;
- manual dispatch with parameters; and
- dependency chaining from one Smith run to bounded follow-up subtasks.

Required job fields:

- stable job ID, owner, and repository/org scope;
- trigger(s), concurrency policy, timeout, retry policy, and max spend;
- prompt, goal, command entry point, or process-skill reference;
- model-routing policy and budget ceiling per step;
- sandbox/profile/execution environment;
- required secret scopes;
- GitHub permissions and PR/merge policy; and
- retention/export policy for logs, artifacts, and snapshots.

Example shape:

```yaml
id: implement-labeled-issue
version: 1
owner: maintainer
repository: tonitienda/agent-smith

triggers:
  - github.issue_labeled:
      label: implementation
  - github.pr_labeled:
      label: implementation

concurrency:
  key: repo:${repository}:implementation
  limit: 1

steps:
  - id: implement
    uses: agent.implement
    role: implementation
    provider_policy: anthropic-implementation
    budget: 4.00
  - id: review
    uses: agent.review
    role: pr-review
    provider_policy: gpt-review
    budget: 2.00
  - id: create-or-update-pr
    uses: github.create_or_update_pr
  - id: mark-generated
    uses: github.add_label
    label: smith-generated
  - id: enable-auto-merge
    uses: github.enable_auto_merge
    when: policy.auto_merge_allowed

permissions:
  github:
    contents: write
    pull_requests: write
    issues: write
    checks: read

merge_policy:
  mode: auto
  required:
    - pr_author_is_smith
    - required_checks_green
    - label_present: smith-generated
    - label_present: smith-auto-merge
  forbidden:
    - unknown_checks
    - branch_protection_bypass
    - force_push
```

### 4.2 Deterministic GitHub automation

Smith should support GitHub automation early because the first dogfood loop is "Smith implements Smith".

Supported deterministic GitHub actions:

- normalize incoming webhook events into Smith trigger events;
- read issues, PRs, labels, checks, branch protection, and changed files;
- create branches and PRs from sandbox/worker output;
- update an existing Smith-authored PR on rerun;
- add/remove labels from issues and PRs as explicit workflow steps/hooks;
- comment status summaries and link run/session artifacts;
- set commit statuses or checks for Smith runs;
- enable auto-merge or merge only when repository policy allows it; and
- never force-push, bypass protections, or merge with failed/unknown checks.

Labels are workflow data, not prompt text. For example, the job spec may say `github.add_label: implementation` or `github.add_label: smith-generated`; the model should not have to remember that instruction.

### 4.3 Always-on orchestrator and run store

The first implementation can run as `smithd`, `smith runs daemon`, or a similarly named long-lived process. It should be deployable on a private VPC host with a small persistent DB such as SQLite, so the maintainer does not need to keep a laptop open 24/7.

Minimum responsibilities:

- load and validate job specs;
- subscribe to schedules and GitHub webhook events;
- enqueue runs and preserve idempotency keys;
- claim/execute one or more jobs with bounded concurrency;
- persist job/run state, retries, terminal outcomes, and audit entries;
- write append-only Smith sessions for every run;
- surface clear failures for missing permissions, unavailable secrets, insufficient budgets, unknown checks, or blocked merge policies; and
- allow manual pause, resume, rerun, cancel, and inspect operations.

### 4.4 Sandbox orchestration

Each run/subtask eventually receives an isolated sandbox, initially targeting a private VPC worker host with either rootless containers for faster product validation or Firecracker/microVM isolation behind the same interface.

Minimum sandbox properties:

- immutable base image selected by job profile;
- fresh writable workspace per subtask;
- repository checkout using short-lived GitHub credentials;
- explicit network egress policy;
- bounded CPU, memory, disk, wall-clock time, and process count;
- captured stdout/stderr, tool traces, file diffs, and run artifacts;
- teardown that revokes credentials and destroys or snapshots the workspace; and
- host-level telemetry for queue latency, boot latency, resource usage, and failures.

### 4.5 Secret and credential provisioning

Smith stores secret metadata in the orchestrator/control plane and keeps secret values in an encrypted backing store or delegated secret manager. Jobs declare named secret scopes; the scheduler resolves them to just-in-time credentials for the runner/sandbox.

Secret classes:

- model-provider credentials for OpenAI, Anthropic, Gemini/Grok/compatible providers;
- GitHub App installation tokens or scoped GitHub credentials for repository read/write, PR, issue, and checks access;
- optional user/team secrets explicitly allowed by policy; and
- Smith service credentials for uploading event logs and artifacts.

Controls:

- no plaintext secrets in job specs or the Smith event log;
- redaction-at-capture remains active before logs leave the runner/sandbox;
- every injection is logged as metadata: name, scope, expiry, recipient runner/sandbox, never value;
- credentials expire quickly and are revoked on teardown when supported; and
- policies can deny secrets to unreviewed job definitions.

Secrets require a dedicated spike before implementation. The spike should compare how tools such as Anthropic/Claude Code, OpenAI/Codex, Cursor, Coder, Ona, and similar agent workflow systems handle repository credentials, model credentials, environment variables, secret redaction, and sandbox access.

### 4.6 Control plane, API, and UI

The first UI can be minimal or deferred, but the architecture should preserve a seam for an operator surface. The orchestrator/control plane owns schedule definitions, run queues, sandbox leases, secret metadata, GitHub installations, policy, and artifact indexes. It exposes:

- API endpoints for CRUD over jobs, manual dispatch, run status, artifacts, and cancellation;
- a worker protocol for sandbox hosts to claim work and stream events;
- a UI for job definitions, run history, cost, PR links, approval gates, and failures;
- import/export of job definitions so cloud automation can be code-reviewed; and
- local Smith CLI commands that can validate a job spec and dispatch/cancel runs.

## 5. MVP slices

### MVP 0 — Smith implements Smith from GitHub triggers

The first dogfood milestone should run privately for the Agent Smith repository and prove the automation loop before solving the full hosted-cloud problem:

1. Versioned `.agent-smith/jobs/*.yaml` job specs.
2. GitHub trigger support for issue/PR label events and PR merged events.
3. Deterministic GitHub action steps/hooks for label mutation, PR creation/update, status/comment reporting, and guarded auto-merge.
4. A local or VPC-hosted always-on daemon with SQLite-backed job/run state.
5. Provider routing per workflow step, so implementation, review, manual-test, and architecture-check roles can use different providers/models on different schedules.
6. Clear failure when permissions, checks, branch protection, secrets, or budgets do not allow the requested action.
7. Every run persisted as a normal Smith append-only session.

### MVP 1 — Private always-on deployment

1. Deploy the same orchestrator on a private VPC/Hetzner host.
2. Add durable webhook delivery, daemon health checks, log retention, backup/restore, and runbook operations.
3. Store secrets in an encrypted backing store or delegated secret manager chosen by the secrets spike.
4. Keep single-tenant/private boundaries while preserving interfaces that could later support hosted users.

### MVP 2 — Disposable remote workers/sandboxes

1. Add a worker protocol for claiming runs, streaming events, uploading artifacts, and reporting terminal status.
2. Add rootless-container or microVM sandbox backend behind the same interface.
3. Enforce resource limits, egress policy, short-lived GitHub credentials, teardown, and artifact/session upload.

### MVP 3 — Hosted execution product path

1. Add GitHub App onboarding for selected repos/orgs.
2. Add operator UI/API for schedules, runs, costs, approvals, artifacts, and failures.
3. Add multi-tenant account/org boundaries only after the single-tenant dogfood system is stable.

## 6. Open questions

1. **Control-plane boundary:** should the first orchestrator live in this repo under new `cmd/smithd` / `internal/orchestrator` packages, or should there be a separate Smith service repository from the beginning?
2. **Deployment shape:** should MVP 0 run as a local daemon first, a private VPC daemon first, or support both from the first ticket?
3. **GitHub credentials:** is a GitHub App required for MVP 0, or can dogfood begin with a tightly scoped token while the GitHub App integration is designed?
4. **Runtime substrate:** is Firecracker on Hetzner the first sandbox implementation, or do we start with a rootless container backend behind the same sandbox interface to validate product flows sooner?
5. **Job-spec source of truth:** are production jobs edited only through repo PRs, or can the UI/API edit jobs and export them back to repo?
6. **Secret store:** use a managed store, age/sops-backed encrypted records, HashiCorp Vault, or cloud-provider KMS equivalent for the first dogfood deployment?
7. **Auto-merge policy:** what exact conditions are sufficient for Smith to merge its own PRs: green CI only, code-owner approval, Smith-authored labels, maintainer opt-in per job, changed-file allowlists, or some combination?
8. **Multi-tenancy:** do we design account/org boundaries now, or keep dogfood single-tenant while preserving interfaces?
9. **Cost ceiling:** what is the initial monthly and per-run dogfood budget?
10. **Vendor selection:** how explicit should vendor/model choice be per step versus delegated to skills/subagent routing policy?

## 7. Required ticket wave

This PRD is decomposed into AS-144 through AS-158:

1. AS-144 — Smith Cloud architecture and threat-model spike.
2. AS-145 — Scheduled job spec and trigger semantics.
3. AS-146 — Cloud control-plane run queue and worker protocol.
4. AS-147 — Sandbox provider interface and disposable microVM prototype.
5. AS-148 — Secret provisioning and redaction contract for cloud sandboxes.
6. AS-149 — GitHub App integration and repository permission model.
7. AS-150 — PR automation and guarded auto-merge policy.
8. AS-151 — Cloud run event-log/artifact ingestion.
9. AS-152 — Dogfood job pack for the Agent Smith repository.
10. AS-153 — Cloud operator UI/API for schedules, runs, costs, and approvals.
11. AS-154 — Always-on dogfood orchestrator daemon and SQLite run store.
12. AS-155 — Deterministic GitHub trigger and label-action hooks.
13. AS-156 — Smith implements Smith workflow pack.
14. AS-157 — Multi-provider workflow role routing for scheduled jobs.
15. AS-158 — Competitive agent workflow, sandbox, and secrets research spike.
