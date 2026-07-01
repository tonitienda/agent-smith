---
id: AS-172
title: Team credential gateway and cross-session prompt/tool audit trail
status: Pending Debrief
area: security
priority: P2
depends_on: [AS-016, AS-017, AS-115, AS-154, AS-165]
source: docs/project/competitors.md
---

# AS-172 · Team credential gateway and cross-session prompt/tool audit trail

## Description

Coder's "AI Governance" add-on centralizes LLM authentication (an AI Gateway
so individual workspaces never hold provider API keys), plus an audit trail of
prompts and tool invocations and policy enforcement across every developer's
agent usage in the deployment. Smith already solves the equivalent problems
per-run and per-orchestrated-job: AS-017 does local OS-keychain key storage
for a single user's interactive sessions, AS-154 defines a secret/redaction
contract for *orchestrated jobs*, and AS-165 ledgers *cost* across background
activity. None of those give a team lead or org admin a single place to see
"which provider keys are in use across every teammate's local Smith
installs" or "what did every agent session do this week," independent of
whether the work ran through the orchestrator.

This ticket scopes whether Smith should add a **team-facing layer**: an
optional credential proxy so individual seats can run without holding a raw
provider API key, and a cross-session audit export (redacted prompts, tool
invocations, model/provider used) that a team can centralize without Smith
itself hosting a multi-tenant service — consistent with D9 (Smith is not a
hosted sandbox for strangers) and the "premium/opt-in for companies" framing
already used in the distributed-execution-zones draft.

## Acceptance criteria

- [ ] Decide whether this is in scope at all, or folds into the distributed
      execution zones PRD's existing Policy Envelope / secrets sections
      (§5.4, §9) instead of shipping as a separate surface.
- [ ] If in scope: define the credential-proxy contract (who issues short-
      lived tokens, how a local Smith instance authenticates to the proxy
      instead of storing a raw key) and how it composes with AS-017's
      keychain storage for users who don't opt in.
- [ ] Define the audit export format (redacted per AS-115, itemized like
      AS-165) and who can access it — local-export-only for V1, no Smith-
      hosted aggregation service.
- [ ] Explicit non-goal: Smith does not become a multi-tenant hosted control
      plane; any aggregation point is owned and run by the team, not Smith.

## Debrief questions

- Is this genuinely distinct from AS-154 (orchestrated-job secrets) and
  AS-165 (cost ledger) once both land, or does it collapse into "point an
  external SIEM/log sink at the append-only event log" with no new code?
- Does a credential-proxy feature belong in Smith core at all, or is it
  strictly the kind of premium/enterprise add-on flagged as opt-in in
  `distributed-execution-zones-prd-draft.md`?
- What's the smallest V1 that earns the audit-trail claim without building an
  enterprise console Smith doesn't otherwise need?

## Dependencies

[AS-016, AS-017, AS-115, AS-154, AS-165]
