---
id: AS-147
title: GitHub event ingestion and deterministic hooks
status: done
area: integrations
priority: P2
depends_on: [AS-160, AS-161]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-147 · GitHub event ingestion and deterministic hooks

## Description

Add the GitHub event and deterministic hook layer needed for Smith to react to labels and PR lifecycle events without encoding workflow state changes inside prompts.

## Clarification (resolved 2026-06-30)

This ticket carried no ticket-local open question — it was held at
`needs-clarification` only because the [orchestrator ADR](../../architecture/orchestrator-architecture.md)
(AS-159) had not yet fixed the architecture it builds on. AS-159 is now
**Accepted** and both dependencies (AS-160 job-spec DSL, AS-161 daemon/scheduler/
run store) are **done**. The ADR's D-ORCH-4 boundary table already places this
work: "GitHub integration | `internal/orchestrator` (AS-147/149) | Normalize
webhooks → trigger events; deterministic action steps." The acceptance criteria
already fully specify the trigger-record shape (repository, issue/PR number,
labels, actor, event time, delivery ID, idempotency key) and the hook surface
(label add/remove, comment, status). No remaining product decision blocks
implementation.

## Acceptance criteria

- [x] Webhook/event normalizer maps issue labeled, PR labeled, PR merged, and comment command events into stable Smith trigger records. — `Normalize` in `internal/orchestrator/webhook.go`.
- [x] Trigger records include repository, issue/PR number, labels, actor, event time, delivery ID, and idempotency key. — `GitHubEvent` carries them; a GitHub-triggered run now persists the targetable subset in the additive `runs.trigger_context` column so a hook can recover the issue/PR after the run is claimed.
- [x] Deterministic hooks can add/remove labels, comment summaries, and update statuses as explicit workflow steps. — `github.add_label`/`github.remove_label`/`github.comment`/`github.set_status` run through the `GitHubActions` port (`internal/orchestrator/hooks.go`), driven by `SessionExecutor` at the `on_start`/`on_success`/`on_failure`/`on_cancel` lifecycle points.
- [x] Hook execution records success/failure in the run DB and append-only Smith session. — each action is appended as an `orchestration_github` block with `outcome` ok/failed; an `on_start` hook failure fails the run closed (terminal outcome in the run store).
- [x] Duplicate webhook delivery does not enqueue duplicate effective work. — `EnqueueGitHub` keys on the delivery id via the store idempotency table.
- [x] Prompt content is not responsible for remembering labels or workflow state transitions. — triggers match on the normalized record and hooks are declarative `github.*` steps; no state lives in prompt text.

## Boundary (not silent punts, per D0)

- The **authenticated transport** implementing the `GitHubActions` port against the
  live GitHub API is **AS-148** (scoped token in a proxy outside the runner, push
  restricted to the run's own branch). This ticket delivers the port + the hook
  runner; the port is injected once AS-148 lands the client, so an orchestrator
  without credentials still runs every job's cognitive work with hooks skipped.
- The **PR-lifecycle** actions (`github.create_or_update_pr`, `github.enable_auto_merge`,
  `github.merge`) are **AS-149**; they are recognised by the DSL catalogue but not
  executed by this hook runner.
- `when`-guarded hook steps are skipped **fail-closed** until the policy engine
  (**AS-157/AS-152**) can evaluate `policy.*`/`trigger.*`/`steps.*` guards — a
  side effect on an unevaluated guard would be unsafe.
- Rich named comment templates (`body_template: run-summary`) render a minimal
  deterministic line here; the full run-summary body is **AS-149**.

## Dependencies

[AS-160, AS-161]
