---
id: AS-080
title: "Spike: hosted multi-tenant live-agent sandboxing"
status: needs-clarification
github_issue: null
depends_on: [AS-077, AS-059]
area: security
priority: P3
source: PRD.md §9, D9, §10 Q12; GUI grilling session 2026-06
---

# AS-080 · Spike — hosting a live agent for strangers

**Status: needs clarification (spike)**

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

## Open questions (why this needs clarification)

1. **Is a live hosted demo even in scope, or does AS-079 (read-only inspector
   with canned sessions) satisfy the "let strangers try it" goal?** If the
   inspector suffices, this spike closes as "won't do for now."
2. **Isolation model** if pursued: container/microVM per session, ephemeral
   filesystem, network egress policy, CPU/mem/time quotas — what's the minimum
   that makes untrusted execution defensible?
3. **Keys & cost:** whose API keys run the demo? A keyless / bring-your-own-key /
   rate-limited shared-key model, and abuse/cost controls.
4. **Tool surface:** does the demo expose only a read-only / no-network tool
   subset, and is that still a useful product demo?
5. **Relationship to AS-059** (plugin trust & sandboxing): can both share one
   isolation substrate, or are they different threat models?

## Acceptance criteria (draft, confirm after clarification)

- [ ] A written recommendation: pursue hosted live demo, or close in favour of
      AS-079, with the threat model that drove the call.
- [ ] If pursued: a concrete isolation design (per-session boundary, key model,
      tool subset, quotas) and the follow-on AS-NNN tickets it spawns.
- [ ] PRD/D9 posture statement updated to reflect the decision (the punt stays
      documented either way).

## Dependencies

- AS-077 (the server that would be hosted), AS-059 (plugin trust & sandboxing
  spike — shared isolation thinking).
