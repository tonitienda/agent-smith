# Smith Cloud dogfood PRD — scheduled autonomous work

> Status: **Draft v0.1** · Owner: Toni · Date: 2026-06-25
>
> Goal: make Smith Cloud capable of replacing the current Assembly Line / Claude Code Routines workflow for implementing Agent Smith itself, while preserving Smith's provider-neutral event-log substrate and making hosted execution an explicit, audited product surface rather than an accidental extension of local `smith serve`.

## 1. Problem and target outcome

Agent Smith already has a local async runner and delegated tasks, but the current product posture deliberately keeps live hosted agents out of scope: local `smith serve` is not a sandbox, and stranger-driven hosted execution collides with the D9 security stance. The next layer is different: a **Smith Cloud control plane** that runs scheduled and event-triggered coding work in isolated, short-lived sandboxes with explicit repository access, scoped secrets, and auditable results.

The immediate dogfood objective is to replace the maintainer's external Assembly Line / Claude Code Routines setup for Smith's own development:

- schedule recurring Smith jobs, such as backlog grooming, nightly quality sweeps, flaky-test triage, dependency review, or documentation drift checks;
- fan each scheduled or event-triggered job into one or more isolated subtasks;
- run each subtask in a fresh sandbox with only the repositories and secrets it needs;
- open, update, optionally merge, and report on GitHub pull requests; and
- store every run as a normal Smith append-only session so the local product wedges (`/context`, `/cost`, `/insights`, replay, compliance archive) remain useful.

## 2. Users and non-goals

### Primary users

- **Maintainer dogfooder:** wants Smith to work on Smith itself without relying on Claude Code Routines or a bespoke Assembly Line.
- **Trusted team admin:** connects a GitHub organization, model-provider accounts, and repository policies to scheduled autonomous work.
- **Reviewer/operator:** reviews generated PRs, sandbox logs, run costs, and approval/merge decisions.

### Non-goals for this phase

- General-purpose untrusted compute hosting.
- Long-lived mutable developer machines.
- A plugin marketplace or arbitrary third-party code plugins.
- Training/fine-tuning models.
- Bypassing repository branch protections or human review policies.

## 3. Product principles

1. **Cloud runs are still Smith sessions.** The event log, block schema, cost accounting, provider normalization, insights, and replay contracts are reused rather than forked.
2. **Every subtask gets a disposable sandbox.** No job shares a writable filesystem with another job; sandboxes start from declared images and are destroyed or snapshotted according to retention policy.
3. **Least privilege by construction.** GitHub repository access, model-provider credentials, network egress, and user/team secrets are scoped to the job/subtask and injected just-in-time.
4. **Policy beats automation.** Auto-PR, auto-update, and auto-merge are allowed only when repository policy and branch protections allow them.
5. **Provider-neutral scheduling.** A job chooses model/provider through Smith routing policy; Anthropic/OpenAI-specific concepts do not leak into the schedule spec except as optional provider config.
6. **Auditable dogfood first.** The first hosted deployment can be private and narrow, but its security model must be the one future users can understand.

## 4. Core capabilities

### 4.1 Schedule and trigger model

Smith Cloud supports a versioned job spec stored in the Smith control plane and optionally mirrored in-repo under `.agent-smith/jobs/*.yaml`.

Triggers:

- cron-like schedules with timezone and missed-run policy;
- GitHub events, especially PR merged, issue labeled, comment command, and scheduled repository maintenance;
- manual dispatch with parameters; and
- dependency chaining from one Smith run to follow-up subtasks.

Required job fields:

- stable job ID, owner, and repository/org scope;
- trigger(s), concurrency policy, timeout, retry policy, and max spend;
- prompt, goal, or command entry point;
- model-routing policy and budget ceiling;
- sandbox profile;
- required secret scopes;
- GitHub permissions and PR/merge policy; and
- retention/export policy for logs, artifacts, and snapshots.

### 4.2 Sandbox orchestration

Each run/subtask receives an isolated sandbox, initially targeting Hetzner VPC hosts with Firecracker/microVM isolation or an equivalent rootless microVM/container runtime behind the same interface.

Minimum sandbox properties:

- immutable base image selected by job profile;
- fresh writable workspace per subtask;
- repository checkout using short-lived GitHub credentials;
- explicit network egress policy;
- bounded CPU, memory, disk, wall-clock time, and process count;
- captured stdout/stderr, tool traces, file diffs, and run artifacts;
- teardown that revokes credentials and destroys or snapshots the workspace; and
- host-level telemetry for queue latency, boot latency, resource usage, and failures.

### 4.3 Secret and credential provisioning

Smith Cloud stores secret metadata in the control plane and keeps secret values in an encrypted backing store. Jobs declare named secret scopes; the scheduler resolves them to just-in-time credentials for the sandbox.

Secret classes:

- model-provider credentials for OpenAI, Anthropic, and compatible providers;
- GitHub App installation tokens for repository read/write, PR, issue, and checks access;
- optional user/team secrets explicitly allowed by policy; and
- Smith service credentials for uploading event logs and artifacts.

Controls:

- no plaintext secrets in the Smith event log;
- redaction-at-capture remains active before logs leave the sandbox;
- every injection is logged as metadata: name, scope, expiry, recipient sandbox, never value;
- credentials expire quickly and are revoked on teardown when supported; and
- policies can deny secrets to unreviewed job definitions.

### 4.4 GitHub App and repository automation

Smith Cloud should use a GitHub App rather than user PATs. Users install the App on selected repositories and grant granular permissions.

Supported flows:

- trigger a Smith job when a PR is merged;
- create branches and PRs from sandbox output;
- update an existing Smith-authored PR on rerun;
- comment status summaries and link run/session artifacts;
- set commit statuses or checks for Smith runs;
- optionally enable auto-merge when branch protections, repo policy, and reviewer policy allow it; and
- never force-push, bypass protections, or merge without a policy-allowed signal.

### 4.5 Control plane, API, and UI

The control plane owns schedule definitions, run queues, sandbox leases, secret metadata, GitHub installations, policy, and artifact indexes. It exposes:

- API endpoints for CRUD over jobs, manual dispatch, run status, artifacts, and cancellation;
- a worker protocol for sandbox hosts to claim work and stream events;
- a UI for job definitions, run history, cost, PR links, approval gates, and failures;
- import/export of job definitions so cloud automation can be code-reviewed; and
- local Smith CLI commands that can validate a job spec and dispatch/cancel cloud runs.

## 5. MVP slice

The first dogfood milestone should run privately for the Agent Smith repository:

1. GitHub App installed only on `agent-smith` with least-privilege repo permissions.
2. One Hetzner VPC worker pool with disposable microVM sandboxes.
3. Encrypted secrets for OpenAI/Anthropic plus GitHub App installation tokens.
4. Cron and `pull_request.closed`/merged triggers.
5. Job specs for documentation drift, backlog grooming, and post-merge follow-up PRs.
6. PR creation/update, but auto-merge disabled until policy and audit trails are proven.
7. Event logs and artifacts uploaded into the normal Smith session store shape.

## 6. Open questions

1. **Control-plane boundary:** should the first control plane be a separate service/repo, or live in this repo under new `cmd/smith-cloud` / `internal/cloud` packages until it splits?
2. **Runtime substrate:** is Firecracker on Hetzner the first implementation, or do we start with a rootless container backend behind the same sandbox interface to validate product flows sooner?
3. **Job-spec source of truth:** are cloud schedules edited in UI and exported to repo, or repo PRs are the only write path for production jobs?
4. **Secret store:** use a managed store, age/sops-backed encrypted records, HashiCorp Vault, or cloud-provider KMS equivalent for the first dogfood deployment?
5. **Auto-merge policy:** what exact conditions are sufficient for Smith to merge its own PRs: green CI only, code-owner approval, Smith-authored low-risk labels, or maintainer opt-in per job?
6. **Multi-tenancy:** do we design account/org boundaries now, or keep dogfood single-tenant while preserving interfaces?
7. **Cost ceiling:** what is the initial monthly and per-run dogfood budget?

## 7. Required ticket wave

This PRD is decomposed into AS-144 through AS-153:

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
