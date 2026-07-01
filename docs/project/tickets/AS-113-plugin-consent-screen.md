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

## Clarification (2026-07-01)

Both questions have documented answers; the first is a "wait," which is itself a
resolution but does not unblock implementation.

- **Install flow — wait, do not define a workaround.** `docs/design/plugin-trust.md`
  §6 is explicit that the consent screen applies "when a third-party manifest is
  installed (a future marketplace/`smith plugin add` flow, **not v1**)", and §8 lists
  AS-113 as "Implement §6 when an install flow exists ... blocked by a
  plugin-install/marketplace ticket (§7.26, not yet filed)." §7.26 is listed in
  `docs/project/tickets/README.md` as "Not ticketed (intentionally) ... too far out
  to spec honestly." Defining a minimal manual install here would invent an anchor
  the source design doc deliberately does not specify. A 2026-06-30 QA re-triage
  (`docs/project/tickets/README.md`, "Needs clarification" section) already reached
  the same conclusion and confirmed AS-113 remains blocked on §7.26. This
  clarification pass reconfirms that finding rather than overturning it.
- **Grant persistence — the config layer, via a typed consumer view, no dedicated
  trust store.** `internal/config` is already the substrate for exactly this shape of
  decision: AS-016's permission model persists "'Always allow this' at prompt time
  appends to the project allowlist" through the same layered config. AS-093
  establishes the typed-view convention (`permission.ConfigFrom`, `budget.ConfigFrom`)
  that a future `plugin`/`trust` package should follow (e.g. `plugin.ConfigFrom`) to
  store the prior grant (manifest content hash + granted scope set) read/written
  through `internal/config`. The two other persistence mechanisms in the codebase are
  narrower special cases that don't fit: the OS keychain (AS-017) is reserved for
  secret provider API keys, and the orchestrator's SQLite store (AS-161) is scoped to
  the always-on daemon's run-control state. Neither applies to a non-secret grant
  record.

**Status unchanged.** Q1's answer is "wait for §7.26," so this ticket still has
nothing to hang a consent screen on — it stays `needs-clarification`/blocked rather
than moving to `ready-to-implement`. Re-triage once a plugin-install/marketplace
ticket is filed; at that point Q2's answer (config-layer typed view) is ready to
apply without further discussion.

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
