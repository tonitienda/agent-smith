---
id: AS-171
title: Outbound completion notifications for background & orchestrator runs
status: Pending Debrief
area: async
priority: P2
depends_on: [AS-054, AS-115, AS-132, AS-155, AS-161]
source: docs/project/competitors.md
---

# AS-171 · Outbound completion notifications for background & orchestrator runs

## Description

AS-054 shipped the local async runner but explicitly deferred "webhooks/desktop
notifications" — users must poll `smith runs list/status`. Competitors have
made not-polling the default: Cursor's mobile app pushes a notification when a
background agent finishes, needs input, or is ready for review, and lets a
user steer a running agent remotely from their phone; Ona (acquired by OpenAI,
June 2026) checkpoints long-running cloud agents so work outlives a closed
laptop, with the implicit promise that the user will be told when it's safe to
look. As Smith's async runner (AS-054), daemon (AS-161), and orchestrator wave
(AS-147…AS-157) mature, "did my background job finish, fail, or stall waiting
on a permission gate" becomes a real gap for anyone who kicks off work and
walks away.

This ticket scopes an **outbound notification contract** — not a mobile app.
Smith stays local-first and provider-neutral; the deliverable is a pluggable
sink (webhook POST, desktop notification, and/or a documented event schema
third parties can wire to Slack/email/mobile push) that fires on run
completion, failure, and permission-gate-blocked states, sourced from the
existing event log rather than a new tracking mechanism.

## Acceptance criteria

- [ ] A notification event schema is defined for run completed / failed /
      interrupted / awaiting-permission states, derived from existing
      event-log records (no parallel state store).
- [ ] At least one first-party sink ships (desktop notification via OS-native
      APIs, or a generic webhook POST) configured per-project or per-run.
- [ ] Notification delivery is best-effort and never blocks or gates run
      execution — a failed notification does not fail the run.
- [ ] Secrets/redaction rules from AS-115 apply to notification payloads.
- [ ] Docs cover how to wire the webhook sink to common external channels
      (Slack, email, mobile push) without Smith taking a dependency on any of
      them.

## Debrief questions

- Is a first-party desktop notification worth the OS-specific surface, or
  should V1 ship webhook-only and let external tooling handle desktop/mobile
  delivery?
- Should the operator API (AS-155) expose a "remote steer" action (e.g.
  approve a pending permission gate from a notification), or is that explicitly
  out of scope until a GUI/mobile face exists?
- Does this belong on the local async runner (AS-054) track, the orchestrator
  daemon (AS-161) track, or both, given they currently have separate
  completion-surface stories?

## Dependencies

[AS-054, AS-115, AS-132, AS-155, AS-161]
