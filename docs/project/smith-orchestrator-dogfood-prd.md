# Smith Orchestrator dogfood PRD — always-on deterministic agent work

> Status: **Draft v0.1** · Owner: Toni · Date: 2026-06-25
>
> Goal: replace the maintainer's external Assembly Line / Claude Code Routines workflow for implementing Agent Smith itself with one deterministic, provider-neutral, always-on Smith orchestrator. Cloud is a deployment mode, not the product nucleus.

## 1. Problem and target outcome

Agent Smith already has a local async runner and delegated tasks, but the maintainer workflow for building Smith is scattered across several schedulers and model sessions: GitHub, Anthropic, OpenAI/GPT, and manual hand-offs. The missing layer is an orchestrator that owns schedules, GitHub triggers, workflow state, provider routing, GitHub actions, policy gates, event logs, and run history in one place.

The first product target is **Smith implements Smith**:

- configure recurring, manual, and GitHub-triggered jobs in one place;
- separate schedule/workflow from model/provider selection;
- use different model roles in the same workflow, such as Anthropic for implementation and GPT/OpenAI for PR review;
- support scheduled alternative perspectives, such as architecture checks one day and manual-test simulations another day;
- react to GitHub events such as issue labeled, PR labeled, PR merged, and comment commands;
- perform deterministic GitHub actions such as add/remove labels, create/update PRs, comment summaries, set statuses, and guarded merge;
- fail clearly when permissions, secrets, labels, checks, or budgets are insufficient;
- run as an always-on daemon locally or in a private VPC so the maintainer's laptop does not need to stay open 24/7; and
- persist every run as a normal Smith append-only session so `/context`, `/cost`, `/insights`, replay, and future audit tools keep working.

The longer-term direction may include hosted execution for users, companies, or enterprises, but this PRD intentionally scopes the first wave as private dogfood automation.

## 2. Product principle

Smith is not primarily an AI agent. Smith is a deterministic workflow engine that delegates cognition to AI models.

That means:

- models write code, review, test, and reason;
- Smith owns schedules, labels, retries, permissions, budgets, PR actions, and merge decisions;
- provider choice is explicit workflow/routing policy, not scattered schedules across vendor tools;
- GitHub labels and PR actions are workflow steps/hooks, not prompt instructions;
- every meaningful decision is recorded in the event log or run DB; and
- unsafe or underspecified states fail closed.

## 3. Users and non-goals

### Primary users

- **Maintainer dogfooder:** wants Smith to work on Agent Smith itself without external Assembly Line / Claude Code Routines glue.
- **Reviewer/operator:** reviews generated PRs, run logs, costs, provider outputs, and merge decisions.
- **Trusted team admin:** later connects selected repositories, model-provider accounts, and repository policies.

### Non-goals for this wave

- General-purpose untrusted compute hosting.
- Long-lived mutable developer machines.
- Plugin marketplace or arbitrary third-party plugins.
- Training/fine-tuning models.
- Smith editing its own job specs.
- Jobs creating other jobs.
- Letting prompts decide labels, permissions, retries, merges, or workflow state transitions.
- Bypassing branch protections or repository review policies.

## 4. Core capabilities

### 4.1 Job spec and workflow DSL

Smith supports versioned job specs stored under `.agent-smith/jobs/*.yaml`.

A job spec declares:

- stable job ID, owner, repository/org scope;
- triggers: cron, manual dispatch, GitHub event, or bounded follow-up;
- concurrency, timeout, retry, and budget policy;
- steps, hooks, and deterministic actions;
- model/provider routing per step or role;
- required GitHub permissions and secret scopes;
- PR, label, status, and merge policy; and
- retention/export policy for logs and artifacts.

Example shape:

```yaml
id: implement-labeled-work
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

### 4.2 Always-on orchestrator daemon

The first implementation can be `smithd`, `smith runs daemon`, or a similar long-lived process. It should run locally or on a private VPC host with a small persistent DB, likely SQLite first.

Responsibilities:

- load and validate job specs;
- subscribe to schedules and GitHub webhooks;
- enqueue runs with idempotency keys;
- execute with bounded concurrency;
- persist run state, retries, terminal outcomes, and audit entries;
- write append-only Smith sessions;
- report clear failures; and
- support pause, resume, rerun, cancel, inspect, and health operations.

### 4.3 Deterministic GitHub automation

GitHub automation is part of MVP 0 because Smith must implement Smith.

Supported flows:

- normalize GitHub webhook events into Smith trigger events;
- trigger jobs on issue/PR labels such as `implementation`;
- add/remove labels through explicit workflow steps/hooks;
- create/update Smith-authored branches and PRs;
- comment run summaries and link artifacts/sessions;
- read checks, branch protection, changed files, and labels;
- enable auto-merge or merge only when policy allows it; and
- never force-push, bypass protections, or merge on failed/unknown checks.

### 4.4 Multi-provider workflow routing

The orchestrator separates role from provider. A workflow can use Anthropic for implementation, GPT/OpenAI for review, and another provider/model for architecture checks, image tasks, or manual-test simulation. Provider choice can be explicit per step or delegated to a named routing policy, skill, or subagent configuration.

### 4.5 Event-log and run-store integration

Every orchestrated run must produce a normal Smith session, with extra metadata for job ID, trigger, provider role, GitHub refs, PR links, run DB ID, artifacts, and policy decisions. Cost accounting and insights should reuse existing Smith readers instead of introducing a second observability path.

### 4.6 Secrets, credentials, and sandboxing

Secrets and sandboxing should be researched before implementation but designed into the seam from the beginning.

Desired properties:

- no plaintext secrets in job specs or event logs;
- short-lived GitHub credentials where possible;
- explicit declared secret scopes;
- redaction-at-capture before logs leave a runner;
- clear failure when a job lacks permission or secret scope;
- rootless container or microVM execution behind a common sandbox interface later; and
- deployability to a private VPC/Hetzner host before a hosted multi-tenant product.

## 5. MVP slices

### MVP 0 — Smith implements Smith from GitHub triggers

1. `.agent-smith/jobs/*.yaml` job specs.
2. GitHub issue/PR label triggers and PR-merged triggers.
3. Deterministic GitHub action steps for labels, PR creation/update, status/comment reporting, and guarded merge.
4. Always-on daemon with SQLite-backed job/run state.
5. Provider routing per workflow step.
6. Clear fail-closed behavior for permissions, checks, branch protection, secrets, and budgets.
7. Every run persisted as a Smith append-only session.

### MVP 1 — Private VPC dogfood deployment

1. Deploy the same orchestrator on a private VPC/Hetzner host.
2. Add durable webhook delivery, daemon health checks, backups, logs, and runbooks.
3. Add selected secret-store backend based on the research spike.
4. Keep single-tenant/private boundaries while preserving seams for future hosted use.

### MVP 2 — Remote workers and sandboxes

1. Add worker claim/heartbeat/stream/finish protocol.
2. Add rootless-container or microVM backend behind a sandbox interface.
3. Enforce resource limits, egress policy, teardown, short-lived GitHub credentials, and artifact upload.

### MVP 3 — Hosted execution path

1. Add GitHub App onboarding for selected repos/orgs.
2. Add operator UI/API for jobs, runs, costs, approvals, artifacts, and failures.
3. Add multi-tenant account/org boundaries only after dogfood stabilizes.

## 6. Open questions

1. Should the first daemon be `smithd`, `smith runs daemon`, or another command shape?
2. Should MVP 0 use a GitHub App immediately, or a tightly scoped maintainer token while the App design is spiked?
3. Which SQLite schema is the minimum viable run store, and which events stay only in the Smith session log?
4. Are production job specs repo-only, or can the future UI edit them and export to repo?
5. What exact auto-merge policy is acceptable for Smith-authored PRs?
6. Which provider-routing decisions are explicit per step versus delegated to skills/subagents?
7. Which secrets approach should dogfood use first?
8. Which sandbox backend should be first: none/local checkout, rootless container, or microVM?
9. What is the first monthly and per-run budget ceiling?

## 7. Ticket wave

This PRD is decomposed into AS-144 through AS-158:

1. AS-144 — Orchestrator architecture and product boundaries.
2. AS-145 — Job specification and workflow DSL.
3. AS-146 — Daemon, scheduler, and SQLite run store.
4. AS-147 — GitHub event ingestion and deterministic hooks.
5. AS-148 — GitHub authentication strategy.
6. AS-149 — PR lifecycle automation.
7. AS-150 — Multi-provider workflow routing.
8. AS-151 — Smith event-log integration for orchestrated runs.
9. AS-152 — Smith implements Smith dogfood workflow pack.
10. AS-153 — Sandbox abstraction and execution environments.
11. AS-154 — Secret management and redaction contract.
12. AS-155 — Operator API/UI.
13. AS-156 — Private VPC deployment.
14. AS-157 — Auto-merge policies and safety gates.
15. AS-158 — Competitive agent workflow, sandbox, and secrets research spike.
