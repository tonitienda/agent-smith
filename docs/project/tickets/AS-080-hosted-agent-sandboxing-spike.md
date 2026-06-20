---
id: AS-080
title: "Spike: hosted multi-tenant live-agent sandboxing"
status: ready-to-implement
github_issue: 135
depends_on: [AS-077, AS-059]
area: security
priority: P3
source: PRD.md §9, D9, §10 Q12; GUI grilling session 2026-06
---

# AS-080 · Spike — hosting a live agent for strangers

**Status: ready to implement (spike)**

## Description

Explicitly flagged, never buried (D0). The GUI grilling surfaced a wish to let
strangers drive a *live* agent on a hosted web instance. That is not a UI
feature — it is **multi-tenant arbitrary code execution** (every stranger's turn
runs shell + file writes on our infrastructure), which collides head-on with the
PRD's stated posture:

> **D9:** "Agent Smith runs with your privileges in your environment; you approve
> actions. It is *not* a sandbox."

The thin-client architecture (AS-077/078) is identical for local and hosted use;
only the **security envelope** differs. Local `smith serve` = your machine, your
risk — fine and already covered. Hosting it for untrusted users requires solving
the sandboxing problem D9 deliberately punted, and is a sibling of the plugin
sandboxing spike (AS-059). This spike decides whether it is viable at all before
any build work — and the likely honest answer for the demo case is "don't host a
live agent for strangers; ship the read-only inspector (AS-079) as the public
demo instead."

## Clarified implementation decisions

- **Scope decision for now:** a live hosted demo for strangers is not in implementation scope. AS-079 (read-only inspector with canned/session-uploaded logs) is the public demo path.
- **Purpose of this spike:** document the threat model and the explicit "not now" recommendation, and only sketch what would be required if the project later revisits hosted live execution.
- **If revisited later:** the minimum bar would include per-session microVM/container isolation, ephemeral filesystem, no ambient secrets, strict egress policy, quotas, and a bring-your-own-key or abuse-capped key model. Those are follow-on product/security decisions, not prerequisites for AS-077/AS-078.
- **Relationship to AS-059:** plugin trust and hosted stranger execution are different threat models. Share terminology where useful, but do not block the local plugin spike on hosted multi-tenancy.

## Acceptance criteria

- [ ] A written recommendation: pursue hosted live demo, or close in favour of
      AS-079, with the threat model that drove the call.
- [ ] If pursued: a concrete isolation design (per-session boundary, key model,
      tool subset, quotas) and the follow-on AS-NNN tickets it spawns.
- [ ] PRD/D9 posture statement updated to reflect the decision (the punt stays
      documented either way).

## Dependencies

- AS-077 (the server that would be hosted), AS-059 (plugin trust & sandboxing
  spike — shared isolation thinking).
