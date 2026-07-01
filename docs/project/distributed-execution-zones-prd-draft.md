# PRD: Distributed Execution Zones and Sandboxed Subagents

Status: **In progress / draft**  
Owner: Agent Smith project  
Last updated: 2026-06-18  
Related themes: sandboxing, subagents, remote execution, CI-style execution, auditability, deterministic orchestration
important: some of the features dracribed here are opt-in for power users or conpanies. Specially remote execution, worker/task/environment definitions, etc. Maybe they become a premium/paid feature. Individuals should still get defaulf local sandboxing for safety without extra configuration. 

---

## 1. Summary

Agent Smith should evolve from “an agent running commands locally” into an **orchestrator that can delegate work to multiple execution zones**.

An execution zone may be:

- the local machine without OS sandboxing,
- the local machine with OS sandboxing,
- WSL2,
- a remote CPU worker,
- a remote GPU worker,
- a private VPC worker,
- a CI-like ephemeral worker,
- a repo-specific worker,
- a vendor/model-specific worker.

Each delegated task should run with an explicit **policy envelope** that defines:

- what it can read,
- what it can write,
- which network domains it can access,
- which secrets it can receive,
- which models/vendors it can use,
- resource limits,
- retry/idempotency behavior,
- expected artifacts,
- audit requirements.

The local Smith instance may remain the primary user-facing orchestrator, even when some subagents execute remotely.

This blurs the separation between “local sandbox” and “remote sandbox.” The proposed model is to treat both as variants of the same concept:

> Every task runs inside an execution zone with declared capabilities and enforced policy.

---

## 2. Problem

AI coding agents increasingly need to execute tasks that do not all belong in the same environment.

Examples:

- A local task needs to inspect the current working tree.
- A test task needs a clean Linux environment.
- A build task needs expensive CPU or memory.
- A model task needs GPU.
- A private integration task needs access to an internal VPC.
- An image-generation task may need to use a specific vendor/model.
- A CI task should run in a clean ephemeral environment.
- A risky shell task should be restricted from reading secrets or writing outside the repo.

If all of these run through a single local agent process, several problems appear:

- too much access is granted to one process,
- tasks cannot be parallelized well,
- specialized hardware is unavailable,
- clean reproducibility is difficult,
- secret handling becomes coarse,
- failures become hard to audit,
- long-running work blocks the local session,
- users cannot easily see where work happened or what was shared.

Agent Smith should provide a more controlled and observable model.

---

## 3. Goals

### 3.1 Functional goals

- Allow Smith to delegate subtasks to local or remote execution zones.
- Allow each execution zone to declare its capabilities.
- Allow each delegated task to specify required capabilities.
- Allow Smith to select an appropriate zone deterministically when possible.
- Support local orchestration with remote execution.
- Support remote or CI-style execution as a future extension.
- Return remote work as artifacts, not as implicit mutations.
- Support parallel execution when safe.
- Support scheduling when resources are limited.
- Support retries where operations are idempotent or safely resumable.
- Preserve an append-only audit trail of distributed execution.

### 3.2 Safety goals

- Avoid giving every agent access to every file, secret, network, and tool.
- Make data sent to remote workers explicit and inspectable.
- Restrict network access by default where possible.
- Inject secrets only when explicitly required.
- Restrict secrets to the narrowest possible task and environment.
- Prefer ephemeral environments for remote execution.
- Avoid remote workers directly mutating the user’s local working tree.
- Make sandbox status visible instead of pretending unsupported platforms are protected.

### 3.3 Product goals

- Keep Smith as the clear orchestrator and user-facing control surface.
- Support deterministic, configuration-driven behavior where possible.
- Allow controlled agent decision-making only inside explicit boundaries.
- Make distributed execution understandable through timelines, artifacts, and audit logs.
- Support future team/enterprise use cases such as private VPC workers and CI integration.

---

## 4. Non-goals

At this stage, this feature does **not** aim to:

- provide a perfect security boundary for arbitrary hostile code,
- replace full VM or microVM isolation for high-risk workloads,
- support native Windows execution,
- automatically upload entire local environments to remote workers,
- allow remote workers to directly mutate the local filesystem,
- hide remote delegation from the user,
- make every orchestration decision agentic or probabilistic,
- solve billing/cost management in detail,
- define the final implementation technology.

Windows native support is out of scope for now. WSL2 may be supported later through the Linux execution backend.

---

## 5. Core concepts

### 5.1 Smith Orchestrator

The Smith Orchestrator is responsible for:

- task planning,
- subtask decomposition,
- worker selection,
- policy construction,
- user permission prompts,
- execution scheduling,
- result merging,
- audit trail generation,
- final review.

The orchestrator may run locally, remotely, or in CI, but the initial target is:

> Local Smith orchestrates local and remote subagents.

### 5.2 Agent

An Agent is a reasoning/execution unit assigned a specific goal.

Examples:

- code exploration agent,
- test runner agent,
- implementation agent,
- code review agent,
- documentation agent,
- image generation agent,
- dependency update agent,
- security review agent.

Agents do not imply a specific physical location. The same agent type may run in different execution zones.

### 5.3 Execution Zone

An Execution Zone is where actions actually happen.

Examples:

- `local`
- `local-sandbox`
- `wsl2-linux`
- `remote-ci-small`
- `remote-ci-large`
- `remote-gpu`
- `private-vpc`
- `repo-specific-worker`
- `vendor-gemini-image`
- `vendor-grok-research`

An execution zone declares:

- operating system,
- available tools,
- available models,
- available repos,
- network profile,
- secret scopes,
- hardware capabilities,
- concurrency limits,
- cost profile,
- sandbox guarantees,
- persistence model.

### 5.4 Policy Envelope

A Policy Envelope describes the boundaries of one delegated task.

It should include:

- filesystem policy,
- network policy,
- secret policy,
- model/vendor policy,
- resource policy,
- retry policy,
- idempotency expectations,
- artifact policy,
- audit policy.

The policy envelope should be recorded in the audit trail before execution starts.

### 5.5 Artifact Boundary

Distributed work should cross environment boundaries through explicit artifacts.

Examples:

- patch files,
- commits,
- branches,
- logs,
- test reports,
- screenshots,
- generated images,
- structured JSON reports,
- dependency graphs,
- benchmark results,
- reasoning summaries,
- failure reports.

Remote execution should not silently mutate local state.

Preferred flow:

1. Smith delegates task to worker.
2. Worker executes in its zone.
3. Worker returns artifacts.
4. Smith validates/reviews artifacts.
5. User or Smith-approved policy applies selected artifacts locally.

---

## 6. Example user stories

### 6.1 Local orchestration with remote test execution

As a user, I want Smith to implement a change locally but run the expensive test suite remotely, so that my laptop is not blocked.

Expected behavior:

- Smith analyzes the repo locally.
- Smith creates or collects a patch.
- Smith sends the patch, repo ref, and task instructions to a remote CI worker.
- The remote worker runs tests in a clean environment.
- The remote worker returns logs and test results.
- Smith summarizes the results locally.
- No remote worker directly modifies my local working tree.

### 6.2 GPU-backed subtask

As a user, I want one subagent to run on a GPU worker, while the rest of the task stays local.

Expected behavior:

- Smith detects or is configured that the task requires GPU.
- Smith selects a `remote-gpu` execution zone.
- Smith sends only the required input data.
- The GPU worker returns generated artifacts or analysis.
- Smith records the data sent, model used, cost estimate if available, and artifacts returned.

### 6.3 Private repo / private VPC subtask

As a user, I want a worker inside my private VPC to run a task requiring access to internal systems.

Expected behavior:

- Smith delegates only that subtask to the private VPC worker.
- The worker receives narrowly scoped credentials.
- The worker can access allowed internal domains.
- The worker cannot access arbitrary external domains unless configured.
- Smith records the worker, network policy, secret references, and returned artifacts.

### 6.4 Hybrid model/vendor execution

As a user, I want Smith to orchestrate the overall task with one model but use another vendor/model for a specialized subtask.

Example:

- Smith uses Claude Code-style orchestration for planning/code.
- A subagent uses Gemini, Grok, or another provider for image generation, multimodal analysis, or research.
- The chosen vendor is explicit in the task policy.
- Data sent to the vendor is recorded in the audit trail.
- The result returns as an artifact.

Expected behavior:

- Smith does not silently switch vendors.
- Vendor/model choice can be configured by the user.
- Agent-driven vendor selection is only allowed if policy permits it.
- Sensitive files/secrets are not sent to external vendors unless explicitly allowed.

---

## 7. Execution model

### 7.1 High-level lifecycle

A distributed task should follow this lifecycle:

1. User asks Smith to perform a task.
2. Smith creates a task plan.
3. Smith identifies subtasks.
4. Each subtask gets:
   - requirements,
   - candidate execution zones,
   - policy envelope,
   - expected artifacts.
5. Smith schedules subtasks.
6. Workers execute subtasks.
7. Workers emit events and artifacts.
8. Smith collects and validates results.
9. Smith merges or compares outputs.
10. Smith asks for approval when needed.
11. Smith applies final changes.
12. Smith writes final audit record.

### 7.2 Task requirements

A subtask may declare requirements such as:

```yaml
requires:
  os: linux
  gpu: false
  memory_mb: 8192
  max_duration: 30m
  repo_access:
    - current
  network:
    - github.com
    - proxy.golang.org
  secrets:
    - github_token_readonly
  models:
    - claude
```

### 7.3 Execution zone declaration

An execution zone may declare capabilities such as:

```yaml
execution_zones:
  remote-ci-small:
    type: remote
    os: linux
    sandbox: container
    max_concurrency: 4
    capabilities:
      gpu: false
      memory_mb: 4096
      network_profiles:
        - no-network
        - package-install
      repos:
        - current
    secrets:
      allowed:
        - github_token_readonly
    persistence:
      workspace: ephemeral
      artifacts: retained
```

### 7.4 Worker selection

Smith should prefer deterministic worker selection.

Possible selection order:

1. User explicitly selected worker.
2. Project configuration maps task type to worker.
3. Policy rules select worker based on requirements.
4. Smith proposes a worker and asks for confirmation.
5. Agentic selection is allowed only if configuration permits.

Example deterministic mapping:

```yaml
task_routing:
  run_tests:
    preferred_zone: remote-ci-small
  image_generation:
    preferred_zone: vendor-gemini-image
  code_edit:
    preferred_zone: local-sandbox
  private_integration_test:
    preferred_zone: private-vpc
```

---

## 8. Configuration vs agent decision-making

Agent Smith should prefer deterministic behavior whenever possible.

### 8.1 Principle

Configuration and policy should define the default behavior. Agents may make recommendations, but should not silently expand their authority.

### 8.2 Deterministic decisions

The following should be deterministic where possible:

- which execution zones exist,
- which secrets each zone may receive,
- which domains each zone may access,
- which vendors/models may be used,
- which task types map to which zones,
- retry limits,
- concurrency limits,
- artifact retention,
- whether remote execution requires confirmation.

### 8.3 Agentic decisions

The agent may be allowed to decide:

- whether a task can be split,
- which subtasks can run in parallel,
- whether a failed task should be retried,
- which artifact looks most promising,
- whether a task needs a stronger environment.

But agentic decisions must remain inside configured boundaries.

Example:

- Allowed: “Choose between `remote-ci-small` and `remote-ci-large` for tests.”
- Not allowed: “Send the repo to an unconfigured external provider.”
- Allowed: “Retry this idempotent test run once.”
- Not allowed: “Retry a payment-affecting command without explicit policy.”

### 8.4 Policy violations

If an agent wants to exceed policy, Smith should produce a permission request:

```text
The subagent wants to use a worker or capability not allowed by current policy.

Requested:
- worker: remote-gpu
- network: huggingface.co
- secret: HF_TOKEN

Current policy:
- worker: local-sandbox only
- network: none
- secrets: none

Approve once / approve for session / deny
```

---

## 9. Secrets and environment setup

### 9.1 Secret principles

Secrets should be:

- explicit,
- scoped,
- short-lived where possible,
- injected only into the task that needs them,
- never copied into prompts unless explicitly intended,
- never included in audit logs as raw values,
- referenced by stable secret IDs,
- redacted from logs and artifacts.

### 9.2 Secret references

A task should request secret references, not raw values:

```yaml
secrets:
  required:
    - id: github_token_readonly
      purpose: clone private repo
    - id: npm_publish_token
      purpose: publish package
      requires_confirmation: true
```

The audit log should record:

```yaml
secrets_used:
  - id: github_token_readonly
    injected_into: remote-ci-small
    raw_value_recorded: false
```

### 9.3 Environment construction

Smith should construct a task environment from:

- base environment,
- allowed toolchain variables,
- task-specific variables,
- secret references,
- network policy,
- working directory,
- artifact output paths.

Example:

```yaml
environment:
  variables:
    GOFLAGS: "-mod=readonly"
    CI: "true"
  secrets:
    GITHUB_TOKEN: secret://github_token_readonly
  network:
    mode: allowlist
    allowed_domains:
      - github.com
      - proxy.golang.org
      - sum.golang.org
```

### 9.4 Network and secrets together

Some secrets are only safe with restricted network access.

Example:

```yaml
secret_policy:
  github_token_readonly:
    allowed_domains:
      - github.com
      - api.github.com
    deny_private_networks: true
    deny_all_other_domains: true
```

This prevents a task with a GitHub token from sending it to arbitrary domains.

### 9.5 Vendor/model secrets

For hybrid vendor usage:

```yaml
model_vendors:
  gemini-image:
    secret: secret://gemini_api_key
    allowed_tasks:
      - image_generation
      - image_understanding
    data_policy:
      allow_source_code: false
      allow_images: true
      allow_user_prompt: true
```

Smith should record when external vendors receive data.

---

## 10. Network policy

### 10.1 Modes

Network policy should support at least:

- `none`
- `inherit`
- `allowlist`
- `ask`
- `vpc-only`
- `package-install`
- `vendor-specific`

### 10.2 Allowlisted domains

Example:

```yaml
network:
  mode: allowlist
  allowed_domains:
    - github.com
    - api.github.com
    - proxy.golang.org
    - sum.golang.org
  deny_private_networks: true
```

### 10.3 Ask mode

In `ask` mode, Smith may pause before first contact to a new domain:

```text
The remote worker wants to access:
  registry.npmjs.org

Reason:
  npm install

Allow once / allow for this task / deny
```

### 10.4 Private network protection

By default, remote workers should not be allowed to reach private/internal networks unless explicitly running in a private VPC zone with configured access.

---

## 11. Failure modes

Distributed execution introduces partial failure. Smith should treat failure as normal.

### 11.1 Failure categories

Possible failures:

- worker unavailable,
- worker busy,
- resource unavailable,
- task timeout,
- network denied,
- secret unavailable,
- sandbox unsupported,
- sandbox degraded,
- tool missing,
- dependency install failed,
- agent crashed,
- model provider failed,
- artifact upload failed,
- result validation failed,
- patch conflict,
- non-idempotent command failed midway,
- remote/local state divergence,
- audit persistence failed.

### 11.2 Failure reporting

Failure should return structured information:

```yaml
failure:
  category: network_denied
  recoverable: true
  retryable: false
  reason: "Domain not allowed by policy"
  requested_domain: "registry.npmjs.org"
  suggested_actions:
    - allow domain for this task
    - switch to worker with cached dependencies
    - run without dependency installation
```

### 11.3 Partial results

A failed subtask may still return useful artifacts:

- logs,
- partial patch,
- failing test report,
- screenshots,
- dependency error,
- diagnostic summary.

Smith should preserve partial results and include them in the audit trail.

### 11.4 Degraded sandbox

If an execution zone cannot enforce the requested sandbox level, it must report degradation before running.

Example:

```yaml
sandbox_status:
  requested: workspace-write-network-none
  actual: workspace-write-network-inherit
  degraded: true
  reason: "Network namespace unavailable"
```

By default, degraded sandbox execution should require explicit approval.

---

## 12. Retries and idempotency

### 12.1 Retry principle

Retries should be safe by default.

Smith should not blindly retry non-idempotent operations.

### 12.2 Idempotency classes

Tasks should be classified:

- `pure_read`
- `rebuildable`
- `workspace_mutation`
- `external_side_effect`
- `unknown`

Examples:

```yaml
idempotency:
  class: rebuildable
  retry:
    max_attempts: 2
    backoff: exponential
```

```yaml
idempotency:
  class: external_side_effect
  retry:
    max_attempts: 0
    requires_confirmation: true
```

### 12.3 Safe retry examples

Usually safe to retry:

- read-only analysis,
- test execution in ephemeral environment,
- build in ephemeral environment,
- dependency graph generation,
- image generation if duplicate cost is acceptable and recorded.

Requires caution:

- publishing packages,
- pushing commits,
- creating tickets,
- sending emails,
- modifying external systems,
- running migrations,
- writing to shared environments.

### 12.4 Idempotency keys

For remote execution, Smith should use idempotency keys for task submissions.

Example:

```yaml
task_execution:
  task_id: task_123
  attempt: 1
  idempotency_key: smith-task_123-policy_hash-input_hash
```

Workers should use idempotency keys to avoid duplicate side effects where possible.

### 12.5 Retry audit

Every retry should be appended to the audit trail:

```yaml
events:
  - type: task_attempt_started
    task_id: task_123
    attempt: 1
  - type: task_attempt_failed
    task_id: task_123
    attempt: 1
    reason: worker_timeout
  - type: task_retry_scheduled
    task_id: task_123
    attempt: 2
    policy: exponential_backoff
```

---

## 13. Scheduling and limited resources

### 13.1 Problem

Some environments may have limited availability.

Examples:

- only one GPU worker,
- only one private VPC worker,
- only one repo-specific checkout lock,
- limited vendor API quota,
- limited CI budget,
- limited concurrency for local CPU.

Smith needs orchestration and scheduling, not just immediate dispatch.

### 13.2 Resource declarations

Execution zones should declare concurrency and resource constraints:

```yaml
execution_zones:
  remote-gpu:
    max_concurrency: 1
    queue_policy: fifo
    cost_profile: high
  private-vpc:
    max_concurrency: 2
    queue_policy: priority
```

### 13.3 Scheduling policies

Possible scheduling policies:

- FIFO,
- priority,
- shortest-job-first,
- dependency-aware,
- cost-aware,
- user-confirmed,
- deadline-aware.

Initial recommendation:

- Use deterministic FIFO with priorities.
- Avoid complex agentic scheduling in early versions.
- Allow user/project config to override.

### 13.4 Task dependencies

Smith should support task graphs:

```yaml
tasks:
  analyze:
    runs_on: local-sandbox
  test:
    runs_on: remote-ci-small
    depends_on:
      - analyze
  gpu_experiment:
    runs_on: remote-gpu
    depends_on:
      - analyze
```

### 13.5 Leases

Workers with scarce resources should use leases:

```yaml
lease:
  resource: remote-gpu
  task_id: task_456
  ttl: 45m
  renewable: true
```

If the worker dies or the lease expires, Smith can reschedule or report failure.

---

## 14. Append-only audit trail

### 14.1 Principle

Distributed execution should be auditable.

Smith should maintain an append-only event log for:

- planning decisions,
- selected workers,
- policy envelopes,
- user approvals,
- data sent,
- secrets referenced,
- domains accessed,
- vendor/model calls,
- task attempts,
- retries,
- failures,
- artifacts produced,
- final application of changes.

### 14.2 Event examples

```yaml
- type: task_created
  task_id: task_123
  parent_task_id: root
  goal: "Run integration tests remotely"

- type: policy_envelope_created
  task_id: task_123
  policy_hash: sha256:...

- type: worker_selected
  task_id: task_123
  worker: remote-ci-small
  selection_mode: deterministic_config

- type: data_exported
  task_id: task_123
  files:
    - go.mod
    - go.sum
    - internal/**
  raw_secret_values_exported: false

- type: secret_injected
  task_id: task_123
  secret_id: github_token_readonly
  destination: remote-ci-small

- type: network_access_requested
  task_id: task_123
  domain: proxy.golang.org
  decision: allowed_by_policy

- type: artifact_received
  task_id: task_123
  artifact_id: artifact_789
  artifact_type: test_report

- type: patch_applied
  task_id: task_123
  patch_id: patch_456
  approved_by: user
```

### 14.3 Tamper evidence

Future versions may make the audit trail tamper-evident by chaining events:

```yaml
event_hash: sha256(current_event)
previous_event_hash: sha256(previous_event)
```

This is not required for the first version, but the event model should not make it hard later.

### 14.4 Human-readable timeline

The audit trail should also be visible as a timeline:

```text
09:41 Smith created plan
09:42 Local agent inspected repository
09:43 Remote CI worker started test run
09:48 Remote CI worker failed: missing dependency
09:49 Smith retried with package-install network profile
09:55 Remote CI worker returned passing test report
09:56 Smith proposed patch application
```

---

## 15. Hybrid vendors and model routing

### 15.1 Problem

Some tasks may be better served by specialized models or vendors.

Examples:

- image generation,
- image understanding,
- large context review,
- code generation,
- research,
- documentation rewrite,
- security review.

Smith may orchestrate with one default model but delegate a subtask to another model/vendor.

### 15.2 Requirements

Vendor/model usage should be:

- explicit,
- configurable,
- auditable,
- policy-constrained,
- sensitive-data-aware.

### 15.3 Vendor policy

Example:

```yaml
vendors:
  claude:
    allowed_tasks:
      - orchestration
      - code_review
      - code_generation
    allow_source_code: true

  gemini:
    allowed_tasks:
      - image_understanding
      - image_generation
    allow_source_code: false
    allow_images: true

  grok:
    allowed_tasks:
      - research
      - ideation
    allow_source_code: false
```

### 15.4 Agent-driven vendor selection

Smith may suggest vendor/model selection, but should not silently send data to a vendor outside the configured policy.

Possible modes:

- `fixed`: always use configured vendor for task type.
- `suggest`: Smith proposes vendor, user approves.
- `agentic-within-policy`: Smith can choose among allowed vendors.
- `disabled`: no external vendor delegation.

Recommended default:

```yaml
model_routing:
  mode: fixed
```

or:

```yaml
model_routing:
  mode: suggest
```

for early versions.

---

## 16. Result validation and merging

### 16.1 Remote outputs are untrusted until validated

Remote worker results should be treated as proposed artifacts.

Smith should validate:

- patch applies cleanly,
- tests pass locally or in trusted worker,
- generated files match expected paths,
- artifacts do not contain secrets,
- logs do not contain raw secret values,
- output size is reasonable,
- task completed within policy.

### 16.2 Competing results

Multiple workers may produce competing artifacts.

Example:

- Agent A proposes implementation 1.
- Agent B proposes implementation 2.
- Agent C reviews both.

Smith should preserve all artifacts and explain why one was selected.

### 16.3 Local application

Changes to the local working tree should happen through local Smith after review.

Possible application modes:

- user manually applies patch,
- Smith applies after confirmation,
- Smith auto-applies if project policy allows,
- Smith creates a branch or commit.

---

## 17. Security and privacy considerations

### 17.1 Data minimization

Remote tasks should receive the minimum data required.

Possible payload levels:

- prompt only,
- selected files,
- patch only,
- branch/ref only,
- full repo checkout,
- artifact bundle.

Smith should show or record which level was used.

### 17.2 Secret minimization

Secrets should be injected only when required.

No raw secret values should be stored in prompts, logs, artifacts, or audit events.

### 17.3 Network minimization

Network should default to restricted modes for remote execution.

### 17.4 Vendor minimization

External vendor calls should respect project policy and user expectations.

Sensitive code, private files, credentials, and internal documents should not be sent to vendors unless explicitly allowed.

### 17.5 Local trust

Local Smith may have broader access than workers, especially when running as the user. This should be clear in documentation.

---

## 18. UX requirements

### 18.1 Visibility

Smith should show where work is running.

Example:

```text
Running distributed task:

✓ local-sandbox      inspected code
→ remote-ci-small    running tests
⏳ remote-gpu         queued
```

### 18.2 Permission prompts

Prompts should appear when crossing important boundaries:

- sending files remotely,
- injecting secrets,
- using a new vendor/model,
- allowing a new domain,
- running with degraded sandbox,
- applying remote patch locally,
- retrying non-idempotent operation.

### 18.3 Explainability

Smith should explain why a worker was selected:

```text
Selected remote-ci-small because:
- task requires Linux
- task requires clean checkout
- no GPU required
- configured route for run_tests
```

### 18.4 Audit access

Users should be able to inspect:

- task graph,
- worker timeline,
- artifacts,
- policy envelopes,
- approvals,
- failures and retries.

---

## 19. Example configuration sketch

This is intentionally provisional.

```yaml
smith:
  distributed_execution:
    enabled: true
    default_mode: local-first

  task_routing:
    code_edit:
      preferred_zone: local-sandbox
    run_tests:
      preferred_zone: remote-ci-small
    image_generation:
      preferred_zone: vendor-gemini-image
    private_integration_test:
      preferred_zone: private-vpc

  execution_zones:
    local-sandbox:
      type: local
      os: auto
      sandbox:
        filesystem: workspace-write
        network: ask
      max_concurrency: 2

    remote-ci-small:
      type: remote
      os: linux
      sandbox:
        filesystem: ephemeral
        network: package-install
      max_concurrency: 4
      secrets:
        allowed:
          - github_token_readonly

    remote-gpu:
      type: remote
      os: linux
      capabilities:
        gpu: true
      sandbox:
        filesystem: ephemeral
        network: allowlist
      network:
        allowed_domains:
          - huggingface.co
      max_concurrency: 1

    private-vpc:
      type: remote
      os: linux
      network:
        mode: vpc-only
      secrets:
        allowed:
          - staging_api_token
      max_concurrency: 2

    vendor-gemini-image:
      type: vendor
      vendor: gemini
      allowed_tasks:
        - image_generation
        - image_understanding
      data_policy:
        allow_source_code: false
        allow_images: true
        allow_user_prompt: true

  retries:
    default:
      max_attempts: 1
    idempotency_classes:
      pure_read:
        max_attempts: 3
      rebuildable:
        max_attempts: 2
      workspace_mutation:
        max_attempts: 1
      external_side_effect:
        max_attempts: 0

  audit:
    append_only: true
    include_policy_envelopes: true
    include_artifact_hashes: true
    redact_secrets: true
```

---

## 20. Open questions

### 20.0 Market signal (research note, 2026-07-01)

OpenAI's June 2026 acquisition of Ona (formerly Gitpod) is a direct competitive
data point for this draft: Ona sells exactly the "execution zone" concept here
(sandboxed, policy-governed environments with an audit trail) plus one
capability this draft doesn't yet name explicitly — automatic ~10-minute
checkpointing so a long-running remote agent survives the initiating machine
going offline. See [docs/project/competitors.md](competitors.md) for the full
writeup and [AS-171](tickets/AS-171-run-completion-notifications.md) for the
narrower, local-first slice (completion notifications) split out so it doesn't
wait on this draft's broader design questions. This does not change any
decision recorded below; it's a signal that the "opt-in premium" framing this
draft already uses is directionally validated by the market.

### 20.1 Product questions

- Should distributed execution be enabled by default or opt-in?
- Should remote execution always require user confirmation?
- Should Smith be able to auto-select remote workers, or only propose them?
- How visible should vendor/model routing be during normal use?
- Should local Smith be considered trusted by default?
- Should remote worker results ever be auto-applied?

### 20.2 Security questions

- What is the minimum acceptable sandbox level for remote workers?
- Should all remote workers be ephemeral?
- Should private VPC workers be allowed to access the public internet?
- Should secrets be injected directly, proxied, or brokered through short-lived tokens?
- Should network allowlists be enforced at worker level, proxy level, or both?
- How should sandbox degradation be handled?

### 20.3 Scheduling questions

- Should Smith support queues in the first version?
- How should scarce workers be prioritized?
- Should users be able to cancel queued/running subtasks?
- Should Smith support deadlines?
- How should cost be represented in scheduling?

### 20.4 Audit questions

- Should the append-only log be local-only initially?
- Should event hashes be implemented from the start?
- Should audit logs be committed into the repo, stored under `.smith`, or kept outside the repo?
- How long should artifacts be retained?
- How should secret redaction be verified?

### 20.5 Implementation questions

- What is the minimal worker protocol?
- Should remote workers pull work or receive pushed tasks?
- Should workers communicate directly with Smith or via a broker?
- Should artifacts be stored locally, remotely, or both?
- Should Smith use existing CI providers as execution zones?
- Should workers be long-lived or per-task ephemeral?

---

## 21. Possible phases

### Phase 0: Design only

- Define vocabulary:
  - orchestrator,
  - agent,
  - execution zone,
  - policy envelope,
  - artifact boundary,
  - audit event.
- Define example configuration.
- Define example audit events.
- Document non-goals and safety posture.

### Phase 1: Local task policy model

- No remote execution yet.
- Add internal model for task requirements and policy envelopes.
- Add local execution zone declaration.
- Add append-only local audit events.
- Add visible sandbox status.

### Phase 2: Local sandboxed execution

- Add local sandbox backend.
- Support workspace-write mode.
- Support basic network policy if platform allows.
- Record sandbox status and degradation.

### Phase 3: Remote worker prototype

- Add one remote worker type.
- Send selected files or patches.
- Return logs and artifacts.
- No direct remote mutation of local workspace.
- Add idempotency key and retry metadata.

### Phase 4: Scheduling and queues

- Add task graph.
- Add resource limits.
- Add max concurrency per execution zone.
- Add simple FIFO/priority scheduling.
- Add cancellation.

### Phase 5: Secrets and network hardening

- Add secret references.
- Add per-task environment construction.
- Add network allowlist policy.
- Add domain approval prompts.
- Add redaction checks.

### Phase 6: Hybrid vendors

- Add model/vendor routing policy.
- Support specialized vendor tasks.
- Record data sent to vendors.
- Add user approval for cross-vendor delegation.

### Phase 7: Private VPC / enterprise workers

- Support customer-managed remote workers.
- Add stronger audit/export features.
- Add organization-level policy templates.
- Add worker attestation or trust reporting if needed.

---

## 22. Draft feature statement

Agent Smith supports distributed execution zones: local or remote environments where subagents can run with explicit capabilities, sandboxing, network rules, secrets, resource limits, and audit requirements. Smith remains the orchestrator, deciding or proposing where each subtask should run, scheduling limited resources, collecting artifacts, and applying results only through controlled review flows. The system prefers deterministic configuration over agentic decisions, while allowing controlled agent recommendations inside policy boundaries.

---

## 23. Draft safety statement

Distributed execution is intended to reduce blast radius, improve observability, and enable specialized execution environments. It is not a guarantee of safe execution for arbitrary hostile code. Smith should expose the active sandbox level, policy envelope, remote data sharing, vendor usage, secret injection, and execution timeline so users can understand and control what happened.
