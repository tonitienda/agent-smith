# Job specification and workflow DSL

> Status: **Accepted (design)** · Ticket: AS-160 · Depends on ADR
> [orchestrator-architecture.md](orchestrator-architecture.md) (AS-159) · Source PRD:
> [smith-orchestrator-dogfood-prd.md](../project/smith-orchestrator-dogfood-prd.md) §4.1
>
> This document fixes the **format** of `.agent-smith/jobs/*.yaml` — the
> declarative, versioned, repo-reviewed unit the orchestrator loads. It is the
> contract; the **loader/validator and run store are AS-161**, GitHub action
> semantics are AS-147/AS-149, routing-policy resolution is AS-150, secret
> resolution is AS-154, and merge gating is AS-157. Where this doc and the draft
> PRD example disagree, this doc wins.

## 1. Scope and non-scope

AS-160 defines **what a valid job spec looks like and what makes one invalid**.
It deliberately does *not* implement:

- the YAML loader, the run store, or the daemon (**AS-161**);
- the runtime behaviour of any `uses:` action (**AS-147/AS-149/AS-150**);
- secret backends or the redaction pipeline (**AS-154/AS-115**); or
- the merge decision engine (**AS-157**).

Those tickets consume this format; they do not redefine it. Keeping the format
frozen first lets the daemon, the action library, and the dogfood workflow pack
(AS-152) be built against a stable target.

The format follows the same principles as the rest of Smith's persisted formats:
**additive-only after first ship** (PRD D2 — `version` is the break valve before
then), **fail-closed** on anything underspecified (ADR D-ORCH-1/D-ORCH-6), and
**deterministic** — every label, permission, retry, route, and merge is declared
data, never prompt output (ADR D-ORCH-6, PRD §2).

## 2. File location and identity

- Specs live under `.agent-smith/jobs/*.yaml`, one job per file, committed to the
  repository they automate. The repo is the source of truth; no UI edits specs
  (ADR Q4, non-goal "Smith editing its own job specs").
- The file name is **not** the identity — the `id` field is (§4.1). The loader
  (AS-161) rejects two files that declare the same `id`.
- A spec is reviewed and merged like any other code change. The daemon only ever
  loads specs that are present on the checked-out ref it is running against.

## 3. Top-level shape

```yaml
id: <stable-slug>          # required, immutable identity
version: <int>             # required, schema version of THIS spec format
owner: <handle>            # required, accountable human
repository: <owner/name>   # required for repo-scoped jobs (xor `org`)
org: <org>                 # required for org-scoped jobs (xor `repository`)
description: <text>        # optional, human summary

triggers:    [ ... ]       # required, >=1
concurrency: { ... }       # required
timeout:     <duration>    # required
retries:     { ... }       # optional, defaults to no retry
budget:      { ... }       # required
permissions: { ... }       # required (may be empty maps, but the block is explicit)
secrets:     [ ... ]       # optional, declared scopes only
routing:     { ... }       # optional named policies referenced by steps
steps:       [ ... ]       # required, >=1
hooks:       { ... }       # optional lifecycle hooks
merge_policy: { ... }      # optional; required when any step enables merge
retention:   { ... }       # optional, defaults applied by daemon
```

Every top-level key is **closed**: an unknown key is a validation error, not a
silently ignored extension (fail-closed). New keys arrive only via this document
plus a `version` bump or an additive optional (D2).

## 4. Field reference

### 4.1 Identity and scope

| Field | Type | Rules |
| --- | --- | --- |
| `id` | string | Required. `^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`. Stable for the life of the job; renaming is a new job. Unique across loaded specs. |
| `version` | int ≥ 1 | Required. The version of *this DSL*, not the job's edit count. The loader refuses a `version` it does not understand. |
| `owner` | string | Required. Accountable handle for audit; not a permission grant. |
| `repository` | `owner/name` | Repo-scoped jobs. Mutually exclusive with `org`. |
| `org` | string | Org-scoped jobs. Mutually exclusive with `repository`. |
| `description` | string | Optional free text. Never interpreted. |

Exactly one of `repository` / `org` must be present (fail-closed: neither, or
both, is invalid).

### 4.2 Triggers

`triggers` is a non-empty list. Each entry is a single-key map naming the trigger
kind. MVP 0 kinds:

| Kind | Args | Meaning |
| --- | --- | --- |
| `cron` | `schedule` (5-field cron), `timezone` (IANA name) | Time-based. `timezone` is **required** — a bare offset or a missing zone is rejected (ADR fail-closed; ambiguous-timezone AC). |
| `manual` | `inputs` (optional typed map) | Operator-dispatched via `smith runs` / future API. |
| `github.issue_labeled` | `label` | Fires when the named label is added to an issue. |
| `github.pr_labeled` | `label` | Fires when the named label is added to a PR. |
| `github.pr_merged` | `base` (optional branch filter) | Fires when a PR merges. |
| `github.comment_command` | `command` | Fires on an allow-listed `/command` comment. |
| `followup` | `of` (step id), `max_runs` (int ≥ 1) | Bounded continuation of a prior run. `max_runs` is **required** — unbounded follow-up is rejected. |

`github.*` triggers are only valid on a job whose `permissions.github` grants at
least read on the relevant resource; the loader cross-checks this (§4.6). Labels
referenced here must be declared by the job (a label the job never adds must
still be listed under `known_labels`, see §4.6) so an unknown label fails
validation rather than silently never firing.

### 4.3 Concurrency

```yaml
concurrency:
  key: repo:${repository}:implementation   # required, interpolated template
  limit: 1                                   # required int >= 1
  on_conflict: queue                         # queue | cancel-running | drop ; default queue
```

`limit` is **required and bounded** — there is no "unlimited" value; omitting it
or setting `0`/negative is invalid (unbounded-concurrency AC). `key` may
interpolate `${repository}`, `${org}`, `${id}`, and trigger inputs
(`${trigger.inputs.*}`); unknown interpolation variables are an error.

**Trigger-specific variables in multi-trigger jobs.** `${trigger.inputs.*}`
resolves only for triggers that declare that input. If a job lists more than one
trigger and its `concurrency.key` (or any `when` guard / step `with`) references
`${trigger.inputs.X}`, then **every** trigger on the job must declare input `X`;
otherwise the spec is **rejected at load** (fail-closed) rather than producing an
undefined value at runtime when fired by a trigger that lacks it. In practice a
key that keys off a manual input belongs on a single-trigger (manual) job, or
each trigger must supply the same input set. This makes interpolation a
load-time-checkable property, never a runtime surprise.

### 4.4 Timeout and retries

```yaml
timeout: 45m            # required duration (see "Duration format" below)
retries:
  max: 2                # int >= 0, default 0
  backoff: exponential  # fixed | exponential ; default exponential
  initial: 30s          # required when max > 0
```

`timeout` bounds a single run; the daemon fails the run closed when it elapses.
Retries re-run the whole job under a fresh idempotency key derived from the run
(AS-161 owns the key scheme).

**Duration format.** Every duration in this DSL — `timeout`, `retries.initial`,
and `retention.*` — uses the grammar `^[0-9]+(s|m|h|d)$`, where `d` = 24h. Note
this is **wider than Go's `time.ParseDuration`**, which has no `d` unit. The
AS-161 loader therefore uses the orchestrator's own small parser (parse the
integer, multiply by the unit, with `d`→24h) rather than `time.ParseDuration`;
authors may write `90d` instead of `2160h`. Anything outside the grammar (bare
ints, compound `1h30m`, fractional units) is rejected.

### 4.5 Budget

```yaml
budget:
  run: 6.00             # required USD ceiling for one run
  monthly: 200.00       # optional rolling ceiling for the job
```

`budget.run` is **required** — a job with no run budget is invalid (missing-budget
AC). Step-level `budget` (§4.7) must sum to ≤ `budget.run`; the loader rejects a
step budget total that exceeds the run ceiling. Enforcement reuses the existing
budget guardrails (AS-041/AS-086); this format only declares the ceilings.

### 4.6 Permissions, known labels, secrets

```yaml
permissions:
  github:
    contents: write
    pull_requests: write
    issues: write
    checks: read
known_labels:
  - implementation
  - smith-generated
  - smith-auto-merge
secrets:
  - github-token
  - anthropic-api-key
  - openai-api-key
```

- `permissions.github` uses GitHub's own resource → access vocabulary
  (`read`/`write`); any other key or value is rejected.
- Every label any trigger or step references must appear in `known_labels`;
  an undeclared label is a validation error (unknown-label AC).
- `secrets` lists **scope names only** — never values. A step that needs a secret
  not listed here fails validation (undeclared-secret-scope AC). Plaintext in a
  spec (anything matching a secret pattern) is rejected at load and redacted at
  capture (AS-154/AS-115). The mapping from scope name to a real credential is
  AS-154's job, not this format's.

**How a step references a secret.** Two mechanisms, both load-time checkable:

  1. **Implicit binding (default).** An action declares which secret scopes it
     needs (e.g. `github.*` actions need `github-token`, `agent.*` steps need the
     API-key scope of their resolved provider). The loader binds those from the
     job's `secrets` list automatically; no syntax in the step. This is the
     normal path and keeps specs free of credential plumbing.
  2. **Explicit interpolation.** Where an action arg must carry a scope by name,
     reference it as `${secrets.<scope-name>}` inside a `with:` value. The value
     interpolated at runtime is the secret's *handle*, never its plaintext.

  In both cases the referenced scope (whether bound implicitly or named via
  `${secrets.*}`) must appear in the job's `secrets` list, or validation rule 9
  fails. `${secrets.*}` is the **only** way a secret enters a step — a literal
  that looks like a credential is rejected (rule 14).

### 4.7 Steps

`steps` is a non-empty ordered list. Steps run in declaration order unless a
`when` guard skips one. Each step:

```yaml
- id: implement                 # required, unique within the job
  uses: agent.implement         # required, must name a known action
  role: implementation          # required for agent.* steps
  provider_policy: anthropic-impl  # optional; names a routing entry (§4.8)
  budget: 4.00                  # optional per-step USD ceiling
  when: policy.auto_merge_allowed  # optional declarative guard
  with: { ... }                 # optional action-specific args
```

Two action families exist, and the distinction is load-time enforced:

- **`agent.*`** — bounded *cognitive* steps (`agent.implement`, `agent.review`,
  `agent.architecture_check`, `agent.manual_test_sim`, …). They require a `role`
  and may carry a `provider_policy` and `budget`. Their *output* never decides
  labels/merges/permissions.
- **`github.*`** — *deterministic* action steps (`github.add_label`,
  `github.remove_label`, `github.create_or_update_pr`, `github.comment`,
  `github.set_status`, `github.enable_auto_merge`, `github.merge`). They take
  declarative args under `with:`, never a `role`. Modelling these as steps — not
  as prose an agent emits — is the core safety property (ADR D-ORCH-6).

All action-specific arguments live under the step's `with:` map. The step's own
keys (`id`, `uses`, `role`, `provider_policy`, `budget`, `when`, `with`) are the
**only** keys allowed directly on a step; any other key — e.g. a bare `label:` at
step level — is rejected by rule 1. Put it under `with:` instead.

`uses` must name an action from the known catalogue; an unknown action is
rejected (unknown-action AC). The action *semantics* are defined by AS-147/AS-149;
this format only fixes the call shape and the agent-vs-deterministic split.

`when` is a **declarative** guard holding **exactly one** boolean identifier from
a closed namespace (`policy.*`, `trigger.*`, `steps.<id>.outcome`). It is boolean
data, not an expression an agent fills in; unknown identifiers are rejected. v1
deliberately supports **no logical operators** (`and`/`or`/`not`) and no
comparisons — a single named predicate only. Compound conditions are expressed by
defining a named `policy.*` predicate (catalogue owned by AS-157/AS-152) rather
than by embedding logic in the spec, keeping `when` trivially parseable and
review-legible. Operators may be added additively in a later `version` if a real
need appears.

### 4.8 Routing

```yaml
routing:
  anthropic-impl:
    provider: anthropic
    model: claude-opus-4-8
  gpt-review:
    provider: openai
    model: gpt-5-review
```

`routing` declares named policies that steps reference by `provider_policy`. A
`provider_policy` on a step that has no matching `routing` entry is an error.
This keeps **role separate from provider** (PRD §4.4): the step says *what role*,
the routing entry says *which provider/model*. The resolution mechanism (named
policy vs. skill/subagent delegation) is AS-150; here it is a static reference
that must resolve at load time.

### 4.9 Hooks

```yaml
hooks:
  on_failure:
    - uses: github.comment
      with: { body_template: run-failed }
  on_success:
    - uses: github.comment
      with: { body_template: run-summary }
```

Hooks are lists of **deterministic** (`github.*`) steps run at lifecycle points
(`on_start`, `on_success`, `on_failure`, `on_cancel`). They obey the same
action-catalogue and permission rules as steps. Agent steps are not allowed in
hooks (a hook is bookkeeping, not cognition).

### 4.10 Merge policy

```yaml
merge_policy:
  mode: auto                          # off | auto | manual ; default off
  required:                           # uniform list of single-key maps
    - pr_author_is_smith: true
    - required_checks_green: true
    - label_present: smith-generated
    - label_present: smith-auto-merge
  forbidden:
    - unknown_checks: true
    - branch_protection_bypass: true
    - force_push: true
```

`required` and `forbidden` are **uniform single-key maps**, never a mix of bare
strings and maps: each item is `predicate: <arg>`, and a nullary predicate
(`pr_author_is_smith`, `unknown_checks`, …) takes `true`. This deserialises in Go
as a plain `[]map[string]Value` with no custom unmarshaler and no string-vs-map
branching. (`label_present` repeats with different args, which a YAML list
permits and a map would not — another reason these are lists, not maps.)

`merge_policy` is **required whenever** a step or hook uses
`github.enable_auto_merge` or `github.merge`; such a step without a policy is
rejected (fail-closed). The predicate names come from a closed catalogue defined
by AS-157; an unknown predicate is an error. The format guarantees the
*forbidden* set can never be emptied below the protection invariants (no
`branch_protection_bypass`, `force_push`, or `unknown_checks` merge is
expressible as allowed).

### 4.11 Retention

```yaml
retention:
  runs: 90d                  # how long run-control rows are kept (AS-161)
  artifacts: 30d             # how long step artifacts are kept
```

Narrative and cost stay in the Smith session log (ADR D-ORCH-4); `retention`
only bounds run-store rows and artifacts. Omitted values take daemon defaults.

## 5. Validation rules (normative)

A spec is **rejected at load** (the daemon refuses to schedule it) when any of:

1. An unknown top-level key, step key, or trigger/action name appears.
2. `id` is missing/malformed or collides with another loaded spec.
3. `version` is missing or unsupported by the loader.
4. Neither or both of `repository`/`org` are set.
5. `triggers` is empty, or a `cron` trigger lacks a `timezone` / uses a bare
   offset, or a `followup` trigger lacks `max_runs`.
6. `concurrency.limit` is missing, `< 1`, or absent (no unbounded concurrency).
7. `budget.run` is missing, or summed step budgets exceed it.
8. A referenced label is absent from `known_labels`.
9. A step references a secret scope not listed under `secrets`.
10. A step's `provider_policy` has no matching `routing` entry.
11. An `agent.*` step omits `role`, or a `github.*` step declares a `role`.
12. A merge-enabling step/hook exists without a `merge_policy`, or a
    `merge_policy` tries to permit a forbidden-invariant action.
13. A `when` guard holds anything other than a single known boolean identifier
    (no operators), or a `concurrency.key`/`with` template references an unknown
    identifier.
14. Any plaintext value matches a secret pattern (fail-closed; secrets are
    names); a `${secrets.*}` reference names a scope absent from `secrets`.
15. A `${trigger.inputs.X}` reference appears on a multi-trigger job whose triggers
    do not all declare input `X` (undefined-at-runtime guard).
16. Any duration field is outside `^[0-9]+(s|m|h|d)$`.
17. A `merge_policy.required`/`forbidden` item is not a single-key map.

Every rejection names the file, the field path, and the rule — specs are
repo-reviewable, so validation must read like a review comment, not a stack
trace.

## 6. Examples

The PRD example in §4.1 is the canonical *implementation* job. The examples
below cover the remaining trigger kinds from the acceptance criteria. They are
illustrative (this ticket ships the format, not running jobs); committing live
`.agent-smith/jobs/*.yaml` waits for the AS-161 daemon.

### 6.1 Cron — scheduled architecture check

```yaml
id: nightly-architecture-check
version: 1
owner: maintainer
repository: tonitienda/agent-smith
description: One architecture-perspective review pass each night.

triggers:
  - cron:
      schedule: "0 3 * * *"
      timezone: Europe/Madrid

concurrency: { key: "repo:${repository}:arch", limit: 1 }
timeout: 30m
budget: { run: 3.00, monthly: 60.00 }

permissions:
  github: { contents: read, issues: write }
known_labels: [architecture-finding]
secrets: [github-token, anthropic-api-key]

routing:
  arch:
    provider: anthropic
    model: claude-opus-4-8

steps:
  - id: review
    uses: agent.architecture_check
    role: architecture
    provider_policy: arch
    budget: 3.00
  - id: report
    uses: github.comment
    with: { body_template: arch-summary }
```

### 6.2 Manual dispatch — bounded manual-test simulation

```yaml
id: manual-test-sim
version: 1
owner: maintainer
repository: tonitienda/agent-smith

triggers:
  - manual:
      inputs:
        scenario: { type: string, required: true }

concurrency: { key: "repo:${repository}:mtsim:${trigger.inputs.scenario}", limit: 1 }
timeout: 20m
budget: { run: 2.00 }

permissions: { github: { contents: read } }
secrets: [anthropic-api-key]
routing: { sim: { provider: anthropic, model: claude-opus-4-8 } }

steps:
  - id: simulate
    uses: agent.manual_test_sim
    role: manual-test
    provider_policy: sim
    budget: 2.00
    with: { scenario: "${trigger.inputs.scenario}" }
```

### 6.3 Issue labeled / PR labeled

See the canonical PRD §4.1 `implement-labeled-work` job — it binds both
`github.issue_labeled` and `github.pr_labeled` on `implementation`, runs an
`agent.implement` → `agent.review` pair on separate providers, and gates merge
behind `merge_policy`.

### 6.4 PR merged — bounded follow-up

```yaml
id: post-merge-followup
version: 1
owner: maintainer
repository: tonitienda/agent-smith
description: After a Smith PR merges, run one bounded follow-up tidy pass.

triggers:
  - github.pr_merged: { base: main }
  - followup:
      of: tidy
      max_runs: 1

concurrency: { key: "repo:${repository}:followup", limit: 1, on_conflict: drop }
timeout: 25m
retries: { max: 1, backoff: exponential, initial: 30s }
budget: { run: 3.00 }

permissions:
  github: { contents: write, pull_requests: write, checks: read }
known_labels: [smith-generated, smith-auto-merge]
secrets: [github-token, anthropic-api-key]
routing: { impl: { provider: anthropic, model: claude-opus-4-8 } }

steps:
  - id: tidy
    uses: agent.implement
    role: implementation
    provider_policy: impl
    budget: 3.00
  - id: open-pr
    uses: github.create_or_update_pr
  - id: mark
    uses: github.add_label
    with:
      label: smith-generated

merge_policy:
  mode: manual
  required:
    - pr_author_is_smith: true
    - required_checks_green: true
    - label_present: smith-generated
  forbidden:
    - unknown_checks: true
    - branch_protection_bypass: true
    - force_push: true
```

## 7. Versioning and evolution

`version` is the only break valve. After AS-161 ships a loader for `version: 1`,
changes are **additive-only** (D2): new optional fields, new trigger/action
kinds, new routing/merge predicates — never a removed or repurposed field. A
genuinely breaking change increments `version` and the loader supports both until
old specs migrate. This mirrors the block-schema additive discipline
([block-schema-union.md](block-schema-union.md)) for the orchestrator's own
persisted format.

## 8. Downstream consumers

| Ticket | Consumes from this format |
| --- | --- |
| AS-161 | Loader + validator (§5), run store, `version: 1` parsing. |
| AS-147 / AS-149 | `github.*` action + trigger semantics, `merge_policy` wiring. |
| AS-150 | `routing` resolution (named policy vs. delegated). |
| AS-151 | Per-run metadata (`id`, trigger, role, refs) into the session log. |
| AS-152 | The concrete dogfood job specs written against this format. |
| AS-154 | `secrets` scope resolution + plaintext rejection. |
| AS-157 | `merge_policy` predicate catalogue + gating. |
