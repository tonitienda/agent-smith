---
id: AS-149
title: PR lifecycle automation
status: done
area: integrations
priority: P2
depends_on: [AS-147, AS-148]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-149 · PR lifecycle automation

## Description

Add the deterministic PR lifecycle actions required for Smith to implement Smith: create a branch, open a PR, update a prior Smith-authored PR, comment run summaries, and hand off to merge policy.

## Acceptance criteria

- [x] Workflow steps can create a Smith-owned branch and open a PR linked to the source issue/run. — `github.create_or_update_pr` body step drives the `PRActions` port (`internal/orchestrator/pr.go`): `EnsureBranch` on the deterministic `smith/<job>/issue-<n>` branch, then `CreatePR` into the base, run through `SessionExecutor` on an otherwise-successful run.
- [x] Reruns can update an existing Smith-authored PR while leaving unrelated branches unchanged. — the head branch is keyed on the trigger's issue number so a rerun resolves the same branch; `FindOpenPR` detects the open PR and takes the `UpdatePR` path. `EnsureBranch` never moves an existing ref, so unrelated branches are untouched.
- [x] PR body/comment includes run summary, job ID, provider roles, budget/cost, artifacts, and session links. — `renderPRBody` builds a deterministic markdown summary (job/run id, provider roles resolved via routing, `$budget/run` + cost, artifact ids, session id) with a `<!-- smith-run: <id> -->` marker for idempotent updates.
- [x] PR actions are recorded in the run DB and Smith append-only session. — each action is a `create_branch`/`open_pr`/`update_pr` `orchestration_github` block via `Recorder.GitHubAction`; the PR URL folds into the session-metadata `RunLink.PRLinks`, and the run store links to that session via `Outcome.SessionID` (the AS-147 recording contract).
- [x] Safety checks confirm the target PR or branch is recognized as Smith-owned before update actions proceed. — `smithOwned` requires the existing PR's head to be exactly the `smith/`-prefixed branch this lifecycle manages; a non-Smith PR is refused fail-closed (run fails `blocked_policy`, an `pr_ownership` decision is recorded, no update fires). The lifecycle only ever creates/looks-up/updates `smith/`-prefixed branches, so it can never edit a human PR.
- [x] Merge and auto-merge are delegated to AS-157 policy rather than prompt instructions. — `github.merge`/`github.enable_auto_merge` steps are not executed here; a `merge_policy` `deferred` policy decision is recorded so the punt is explicit (D0).

## Boundary (not silent punts, per D0)

- The **authenticated transport** implementing the `PRActions` port against the
  live GitHub API is out of scope here, exactly as AS-147 shipped the
  `GitHubActions` port without its transport: AS-148 fixed the credential strategy
  (scoped token in a proxy outside the runner) but shipped only the decision. This
  ticket delivers the port + deterministic lifecycle + recording, offline-tested
  with a fake; the port is injected via `SessionExecutor.WithPRActions` once a
  concrete client lands, so an orchestrator without credentials still runs every
  job's cognitive work with PR steps skipped.
- **Merge/auto-merge execution** is AS-157's policy engine; this ticket records the
  deferral rather than acting.
- `github.create_or_update_pr` is run as a **body step** on a successful run. A
  general step-execution engine for `agent.*` steps is **AS-150/AS-152**; the label/
  comment/status github steps remain AS-147 hook-driven.

## Dependencies

[AS-147, AS-148]

## Clarification (resolved 2026-06-30)

The blocker named here was sequencing, not an open product question: AS-147 and
AS-148 are now both `ready-to-implement` with their design fixed, which is what
this ticket was waiting on.

1. **Idempotency keys.** AS-147's acceptance criteria already define the
   trigger-record shape this ticket consumes — "repository, issue/PR number,
   labels, actor, event time, delivery ID, and idempotency key" — so PR
   create/update/comment/status actions key off that same `(delivery ID,
   idempotency key)` pair rather than defining a second one; "duplicate webhook
   delivery does not enqueue duplicate effective work" (AS-147 AC) is the
   existing guarantee this ticket's actions build on.
2. **Auth.** AS-148's clarified strategy (scoped maintainer token now, GitHub
   App migration later, real credential kept in a proxy outside the runner,
   push restricted to the run's own branch) is the credential path PR actions
   use; no separate auth design is needed here.
