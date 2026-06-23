---
id: AS-113
title: Plugin consent screen + scope→sentence table
status: needs-clarification
github_issue: 379
depends_on: [AS-044]
area: security
priority: P2
source: docs/design/plugin-trust.md §6; spun out of AS-059
---

# AS-113 · Plugin consent screen + scope→sentence table

**Status: needs clarification** *(spun out of the AS-059 plugin-trust spike; blocked on a plugin-install flow that does not exist yet)*

## Description

AS-059 §6 specified an install-time consent screen built from the manifest:
identity + origin, scopes rendered in plain language (high-risk scopes flagged),
what the plugin can do, its budget cap, and update semantics where a scope-widening
manifest update re-prompts (scope escalation = fresh consent) while a narrowing
update applies silently.

This is **display, not enforcement** — it is how a user makes the trust decision the
AS-059 §4.2 residual risks require a human for. It needs:

- A fixed scope→sentence table (one plain-language sentence per permission scope).
- A consent presentation surface (TUI panel, AS-067).
- Re-prompt-on-escalation logic keyed off a stored prior-grant scope set.

## Open questions

- **Install flow.** There is no `smith plugin add` / marketplace install path yet
  (§7.26 is not ticketed). This ticket has nothing to hang a consent screen on until
  that exists. Should AS-113 wait for a plugin-install ticket, or define a minimal
  manual install (drop a manifest in a config dir) to anchor the consent flow?
- **Grant persistence.** Where is a prior grant stored (config layer? a dedicated
  trust store?) so escalation re-prompts can compare scope sets across updates?

## Acceptance criteria

- [ ] (After clarification) A consent screen renders manifest identity + scopes in
      plain language, flagging high-risk read scopes.
- [ ] Consent is bound to the manifest's content hash: any update whose hash differs
      re-prompts (a scope-widening update re-prompts loudly); only an identical-hash
      re-install applies without a prompt. A prompt-only change is a functional change
      and re-prompts, since a declarative plugin *is* its prompt.

## Dependencies

- AS-044 (manifest/registry); AS-067 (TUI panel framework) for presentation;
  blocked by a plugin-install/marketplace ticket (§7.26, not yet filed).
</content>
