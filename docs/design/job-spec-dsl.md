# Job specification and workflow DSL (AS-160)

> Status: **Accepted** · Ticket: AS-160 · Source PRD:
> [smith-orchestrator-dogfood-prd.md](../project/smith-orchestrator-dogfood-prd.md) §4.1 ·
> Architecture: [orchestrator-architecture.md](../architecture/orchestrator-architecture.md)
> (AS-159). Where this spec and the draft PRD disagree, this spec wins; where this
> spec and the AS-159 ADR disagree, the ADR wins.

This document defines the versioned, declarative job-spec format the orchestrator
loads from `.agent-smith/jobs/*.yaml`. It fixes the **schema** and the **validation
contract**; the daemon that loads, validates at runtime, and executes these specs is
**AS-161**, and the loader's YAML-parser dependency is introduced there (see
[Format and dependency](#format-and-dependency)). Per the AS-159 ADR, Smith owns the
deterministic shell — schedules, triggers, GitHub actions, permissions, budgets,
labels, and merge policy are declared here, never decided by prompt text.

## 1. Scope

This spec is the design deliverable for AS-160:

- **In scope:** file location and discovery, format and version policy, every
  top-level field, the trigger set, the step/hook/action model, provider routing,
  permission and secret-scope declarations, PR/merge policy, retention, and the
  normative validation rules a conformant loader MUST enforce.
- **Out of scope (owned elsewhere):** runtime loading and validation enforcement,
  the run store, scheduling, and execution → **AS-161**; GitHub event normalization
  and the action implementations → **AS-147/AS-149**; provider-routing mechanism →
  **AS-150**; secret contract → **AS-154**; sandbox seam → **AS-153**; auto-merge
  policy gates → **AS-157**. This spec declares the *surface* those tickets bind to.

The product non-goals in [ADR-159 D-ORCH-6](../architecture/orchestrator-architecture.md#d-orch-6--non-goals-fail-closed)
apply: a job may not edit its own spec, create other jobs, or let a prompt decide
labels, permissions, retries, merges, or state transitions. The format has no field
that expresses any of those — that is enforced structurally, not by validation.

## 2. File location and discovery

- Specs live under `.agent-smith/jobs/*.yaml`, one job per file, committed to the
  repository and reviewed through normal PRs (AS-159 ADR Q4: repo is the source of
  truth; no cloud UI may be required to read, write, or load a spec — AC5).
- The directory is discovered relative to the repository root the daemon is pointed
  at. Files that do not end in `.yaml` are ignored. A file that fails validation is
  rejected with a clear error and does **not** partially load (fail closed).
- See [`.agent-smith/jobs/README.md`](../../.agent-smith/jobs/README.md) for the
  live directory convention.

## 3. Format and dependency

The on-disk format is **YAML**, as fixed by the AS-159 ADR (D-ORCH-4) and PRD §4.1.
This is a deliberate divergence from [ADR-0002](adr-0002-config-format.md), which
chose JSON for the *layered config substrate* specifically to stay stdlib-only. Job
specs are different: they are **human-authored and human-reviewed** artifacts where
comments, block scalars, and anchors materially help a maintainer reason about
schedules, budgets, and merge gates in review. YAML is worth a parser dependency
here; JSON config is not.

The stdlib-only lean (CLAUDE.md) is scoped to *repo tooling*. Job-spec loading is
product code, so introducing a YAML parser (`gopkg.in/yaml.v3` or equivalent) is
permitted — but it is a **real dependency decision and belongs to AS-161**, the
ticket that adds the loader. This spec flags it explicitly rather than letting it
land silently (PRD D0). Until AS-161, no Go code parses these files.

### Schema discipline (additive-only, PRD D2)

- Every spec declares an integer `version`. `version: 1` is defined here.
- The schema is **additive-only**: new concepts are new optional fields; existing
  fields are never removed, renamed, or repurposed. A `version: 1` loader MUST
  tolerate unknown top-level keys it does not understand by **rejecting the spec
  with a clear "unknown field" error** rather than silently ignoring it — job specs
  govern money and merges, so an unrecognized key is a load-time failure, not a
  tolerated no-op. (This is stricter than config-layer D2 tolerance on purpose; the
  blast radius differs.) Raising `version` is how new required semantics opt in.

## 4. Top-level fields

| Field | Required | Type | Meaning |
| --- | --- | --- | --- |
| `id` | yes | string | Stable, unique job identity. Never reused or renumbered. `[a-z0-9-]+`. |
| `version` | yes | int | Spec format version. `1` today. |
| `owner` | yes | string | Accountable maintainer/role for the job. |
| `repository` | one of `repository`/`org` | string | `owner/repo` scope the job acts on. |
| `org` | one of `repository`/`org` | string | Org scope, when the job spans repos. |
| `description` | no | string | Human summary; surfaced by `smith runs inspect`. |
| `triggers` | yes | list | What starts a run (§5). At least one. |
| `concurrency` | yes | object | `key` (string, may interpolate `${repository}`) + `limit` (int ≥ 1). §6. |
| `timeout` | yes | duration | Wall-clock ceiling per run (e.g. `30m`). |
| `retries` | no | object | `max` (int ≥ 0, default 0) + `backoff` (`fixed`/`exponential`) + `delay`. |
| `budget` | yes | object | Run-level ceiling: `usd` (number > 0). Per-step budgets (§7) must not exceed it. |
| `steps` | yes | list | Ordered units of work (§7). At least one. |
| `hooks` | no | object | Lifecycle hooks: `on_success`, `on_failure`, `on_cancel` → lists of action steps (§8). |
| `provider_routing` | no | map | Named routing policies referenced by steps (§9). |
| `permissions` | yes | object | Declared GitHub permission scopes (§10). |
| `secrets` | no | list | Declared secret **scope names** — never values (§10). |
| `pr_policy` | no | object | Branch/PR conventions for PR-producing jobs (§11). |
| `merge_policy` | no | object | Merge gate (§11). Required only if a step uses `github.enable_auto_merge`/`github.merge`. |
| `retention` | no | object | `runs` (count or duration) + `artifacts` (duration) the run store keeps. |

`${repository}`, `${org}`, and `${trigger.*}` are the only interpolations; they are
substituted by the loader from deterministic run context, never from model output.

## 5. Triggers

A job lists one or more triggers. Each is a single-key object naming the trigger
type. The six required example shapes (AC2):

```yaml
triggers:
  # 1. cron — timezone is REQUIRED and must be an IANA name (no bare offsets).
  - cron:
      expr: "0 7 * * 1-5"
      timezone: Europe/Madrid

  # 2. manual dispatch — operator-initiated via `smith runs rerun`/dispatch.
  - manual: {}

  # 3. GitHub issue labeled
  - github.issue_labeled:
      label: implementation

  # 4. GitHub PR labeled
  - github.pr_labeled:
      label: implementation

  # 5. GitHub PR merged
  - github.pr_merged:
      base: main

  # 6. bounded follow-up — a run may enqueue ONE follow-up of the same job,
  #    decremented from `max_followups`; the chain is bounded and cannot fan out.
  - followup:
      max_followups: 1
```

`followup` is how "keep working until CI is green" is expressed without a job
creating new jobs (ADR-159 non-goal): the run store decrements a counter and refuses
to enqueue past zero (fail closed).

## 6. Concurrency

`concurrency.key` groups runs that must not overlap; `concurrency.limit` caps how
many run at once for that key. `limit` MUST be a finite integer ≥ 1 — there is no
"unlimited". The key may interpolate `${repository}` so per-repo serialization is
expressible (`repo:${repository}:implementation`).

## 7. Steps and deterministic actions

`steps` is an ordered list. Each step has a stable `id` and a `uses` naming what it
does. There are two kinds, and the distinction is load-time structural:

- **Cognitive steps** (`agent.*`) — a model does bounded work (implement, review,
  architecture check, manual-test simulation). They carry `role`, optional
  `provider_policy` (§9), and a `budget.usd`.
- **Deterministic action steps** (`github.*` and other `*.` namespaces) — Smith
  performs a fixed action. They never run a model and never read budget. **Labels,
  PR creation/update, comments, status, and merge are steps — never prompt text**
  (AC3, ADR-159 D-ORCH-1).

Common fields: `id` (required, unique within the job), `uses` (required), `when` (an
optional reference to a **declared policy predicate** such as `policy.auto_merge_allowed`
— never a free-form expression), `budget` (cognitive steps only).

### Deterministic action catalog (`version: 1`)

| `uses` | Effect | Notable inputs |
| --- | --- | --- |
| `github.add_label` | Add a label | `label` (must be a declared/known label) |
| `github.remove_label` | Remove a label | `label` |
| `github.create_or_update_pr` | Create or update the Smith-authored PR | `base`, `title_template` |
| `github.comment` | Post a run-summary comment | `body_template` |
| `github.set_status` | Report a commit status/check summary | `context`, `state` |
| `github.enable_auto_merge` | Enable native auto-merge | gated by `merge_policy` |
| `github.merge` | Merge directly (fallback) | gated by `merge_policy` |
| `agent.implement` | Implementation work | `role`, `provider_policy`, `budget` |
| `agent.review` | PR review | `role`, `provider_policy`, `budget` |
| `agent.architecture_check` | Scheduled architecture pass | `role`, `provider_policy`, `budget` |

The catalog is additive: new actions are new `uses` names; a loader rejects unknown
`uses` (§12). Action *implementations* are AS-147/AS-149.

## 8. Hooks

`hooks.on_success`, `hooks.on_failure`, and `hooks.on_cancel` each hold a list of
deterministic action steps (same shape as §7 action steps) run at the named
lifecycle point. Hooks are deterministic-only — no `agent.*` in a hook — so failure
handling cannot itself burn budget or block on a model.

## 9. Provider routing

Role and provider are separate (PRD §4.4, ADR-159 D-ORCH-4/Q6). A cognitive step
sets a `role` and selects a provider one of two ways:

- **Named policy** — `provider_policy: anthropic-implementation`, defined under the
  top-level `provider_routing` map.
- **Inline** — a step may name a provider/model directly for one-off routing.

```yaml
provider_routing:
  anthropic-implementation:
    provider: anthropic
    model: claude-opus-4-8
  gpt-review:
    provider: openai
    model: gpt-5
```

The routing *mechanism* (resolution, fallback, skills/subagents) is **AS-150**; this
spec only fixes the declaration surface.

## 10. Permissions and secret scopes

- `permissions.github` declares the GitHub scopes the job needs (`contents`,
  `pull_requests`, `issues`, `checks`, …) at `read`/`write`. A step that performs an
  action requiring a scope the job did not declare is a **load-time rejection** (§12)
  — fail closed, no escalation at runtime.
- `secrets` is a list of declared **scope names** (e.g. `github-token`,
  `openai-api-key`). **Values never appear in a spec or a log** (PRD §4.6, secret
  contract AS-154; redaction-at-capture AS-115). A step that references an undeclared
  secret scope is rejected.

## 11. PR and merge policy

`pr_policy` declares branch/PR conventions (branch prefix, title template, draft
default) for PR-producing jobs. `merge_policy` is the gate for
`github.enable_auto_merge`/`github.merge`:

```yaml
merge_policy:
  mode: auto            # auto | manual | off
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

`required`/`forbidden` entries are drawn from a **fixed predicate vocabulary**; an
unknown predicate is rejected. The concrete gate semantics (and any additions) are
**AS-157**; this spec fixes that merging is policy-gated and that bypassing
protection, force-push, and merging on unknown/failed checks are structurally
forbidden, never expressible as "allowed".

## 12. Validation contract (normative)

A conformant loader (AS-161) MUST **reject the whole spec, fail closed, and report a
clear error** when any of the following holds (AC4). It MUST NOT load a partial or
"best-effort" job.

1. **Ambiguous timezone** — a `cron` trigger with a missing timezone, a bare UTC
   offset, or a non-IANA name. IANA names only.
2. **Unbounded concurrency** — `concurrency` missing, or `limit` absent / `< 1` /
   non-integer / "unlimited".
3. **Missing budget** — no top-level `budget.usd`, a cognitive step with no
   `budget.usd`, or step budgets that sum/peak above the run budget.
4. **Unknown label** — `github.add_label`/`remove_label`/`label_present` referencing
   a label not in the job's known-label set.
5. **Unknown action** — a step or hook `uses` not in the §7 catalog for the declared
   `version`.
6. **Undeclared secret scope** — a step referencing a secret scope absent from
   `secrets`.
7. **Undeclared permission scope** — an action requiring a GitHub scope the job did
   not declare under `permissions.github`.
8. **Unknown top-level field** — any unrecognized top-level key (§3 schema
   discipline).
9. **Missing required field** — any §4 `Required: yes` field absent, or neither
   `repository` nor `org` present.
10. **Merge without policy** — a `github.enable_auto_merge`/`github.merge` step with
    no `merge_policy`, or a `merge_policy` predicate outside the fixed vocabulary.

These rules are the contract AS-161 implements and tests; the worked examples in §13
are the canonical conformance fixtures.

## 13. Worked examples

### 13.1 Implement labeled work (issue/PR labeled → implement → review → PR → auto-merge)

```yaml
id: implement-labeled-work
version: 1
owner: maintainer
repository: tonitienda/agent-smith
description: Implement issues/PRs labeled `implementation`, review, open a PR, auto-merge when clean.

triggers:
  - github.issue_labeled: { label: implementation }
  - github.pr_labeled: { label: implementation }

concurrency:
  key: repo:${repository}:implementation
  limit: 1
timeout: 45m
retries: { max: 1, backoff: exponential, delay: 30s }
budget: { usd: 8.00 }

provider_routing:
  anthropic-implementation: { provider: anthropic, model: claude-opus-4-8 }
  gpt-review: { provider: openai, model: gpt-5 }

steps:
  - id: implement
    uses: agent.implement
    role: implementation
    provider_policy: anthropic-implementation
    budget: { usd: 4.00 }
  - id: review
    uses: agent.review
    role: pr-review
    provider_policy: gpt-review
    budget: { usd: 2.00 }
  - id: open-pr
    uses: github.create_or_update_pr
    base: main
  - id: mark-generated
    uses: github.add_label
    label: smith-generated
  - id: enable-auto-merge
    uses: github.enable_auto_merge
    when: policy.auto_merge_allowed

permissions:
  github: { contents: write, pull_requests: write, issues: write, checks: read }
secrets: [github-token, anthropic-api-key, openai-api-key]

pr_policy: { branch_prefix: smith/, draft: false }
merge_policy:
  mode: auto
  required: [pr_author_is_smith, required_checks_green, label_present: smith-generated]
  forbidden: [unknown_checks, branch_protection_bypass, force_push]

hooks:
  on_failure:
    - id: report-failure
      uses: github.comment
      body_template: "Run failed; see linked Smith session."

retention: { runs: 200, artifacts: 30d }
```

### 13.2 Scheduled architecture check (cron with required timezone)

```yaml
id: nightly-architecture-check
version: 1
owner: maintainer
repository: tonitienda/agent-smith
triggers:
  - cron: { expr: "0 3 * * *", timezone: Europe/Madrid }
concurrency: { key: repo:${repository}:arch, limit: 1 }
timeout: 30m
budget: { usd: 3.00 }
provider_routing:
  arch: { provider: anthropic, model: claude-opus-4-8 }
steps:
  - id: arch-pass
    uses: agent.architecture_check
    role: architecture
    provider_policy: arch
    budget: { usd: 3.00 }
  - id: report
    uses: github.comment
    body_template: "Architecture check summary attached."
permissions: { github: { contents: read, pull_requests: read, issues: write } }
secrets: [github-token, anthropic-api-key]
```

### 13.3 Manual dispatch (operator-initiated, single step)

```yaml
id: manual-manual-test-sim
version: 1
owner: maintainer
repository: tonitienda/agent-smith
triggers:
  - manual: {}
concurrency: { key: repo:${repository}:manual-test, limit: 1 }
timeout: 20m
budget: { usd: 2.00 }
steps:
  - id: simulate
    uses: agent.review
    role: manual-test-sim
    budget: { usd: 2.00 }
permissions: { github: { contents: read } }
secrets: [anthropic-api-key]
```

### 13.4 PR merged → bounded follow-up (post-merge tidy, one follow-up max)

```yaml
id: post-merge-followup
version: 1
owner: maintainer
repository: tonitienda/agent-smith
triggers:
  - github.pr_merged: { base: main }
  - followup: { max_followups: 1 }
concurrency: { key: repo:${repository}:post-merge, limit: 1 }
timeout: 25m
budget: { usd: 3.00 }
steps:
  - id: tidy
    uses: agent.implement
    role: implementation
    budget: { usd: 3.00 }
  - id: open-pr
    uses: github.create_or_update_pr
    base: main
permissions: { github: { contents: write, pull_requests: write } }
secrets: [github-token, anthropic-api-key]
pr_policy: { branch_prefix: smith/followup-, draft: false }
```

## 14. Loadability and the AS-161 boundary

A spec is **loadable by the daemon without a cloud UI** (AC5): it is plain YAML on
disk, fully reviewable in a PR, and self-contained (no field requires resolving
external UI state to validate). The daemon (`smith runs daemon`, AS-161) discovers
`.agent-smith/jobs/*.yaml`, applies §12, and either loads the job or rejects it with
a clear error. Nothing here depends on a hosted service.

What AS-160 does **not** do, deferred to the named tickets: the YAML parser
dependency and runtime validator (AS-161), action implementations (AS-147/AS-149),
routing resolution (AS-150), the secret contract (AS-154), and merge-gate semantics
(AS-157). Those tickets bind to the surface fixed here; this spec is the contract,
not the engine.
