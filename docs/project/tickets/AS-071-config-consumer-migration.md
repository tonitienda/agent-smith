---
id: AS-071
title: "Migrate config consumers (flat key=value, permissions, pricing) onto the layered config substrate"
status: done
github_issue: 119
depends_on: [AS-031]
area: foundation
priority: P1
source: AS-031 acceptance criteria (consumer migration), docs/design/adr-0002-config-format.md
---

# AS-071 · Migrate config consumers onto the layered config substrate

**Status: done**

## Description

AS-031 landed the layered config substrate (`internal/config`): nested JSON,
precedence (built-in defaults → env → user → project → flags, the low→high
reading of D-CLI-6), typed accessors,
per-leaf source tracking, unknown-key warnings, and `smith config show`. To keep
that PR lean and low-risk, the **migration of existing config consumers** onto
the substrate was deliberately split out into this ticket (the AS-031 AC
"existing config consumers (permissions, pricing) migrate onto this system").

Three consumers currently parse their own config; this ticket moves them onto
`internal/config` so there is one config file and one precedence story:

- **Flat CLI chain** — `internal/cli/config.go` (`config get`/`set`, the flat
  `.smith/config` `key = value` file, D-CLI-6). Today it coexists with the
  nested `.smith/config.json`. Fold the flat keys (e.g. `model`) into the nested
  config and back `get`/`set` with `internal/config`, or define a migration path
  from the flat file. Keep `config get`/`set` scriptable behaviour intact.
- **Permissions** — `internal/permission` reads `permissions.json` with its own
  `Load`/`LoadLayered`/`Merge`. Move the policy under a `permissions` section of
  the unified config (its `Merge` already follows the same user→project
  precedence this substrate generalizes). Preserve the "always allow this"
  append path (AS-016/AS-019) and atomic writes.
- **Pricing** — `internal/cost` overrides via `$SMITH_PRICING`. Expose pricing
  overrides through the unified config (a `pricing` section) while keeping the
  embedded defaults and the env-file escape hatch working.

Because file formats are additive-only for users too (CLAUDE.md / PRD D2), keep
old files readable (or provide an explicit, documented migration) rather than
silently breaking existing `permissions.json` / `.smith/config` files.

## Acceptance criteria

- [x] `config get`/`set` resolve through `internal/config` (no second flat
      loader), with scriptable get/set behaviour and source reporting preserved.
      The flat `internal/cli/config.go` loader was removed; `set` writes nested
      JSON via `config.SetFileValue` (atomic), `get` resolves dotted keys via
      `config.Value`.
- [x] Permission policy loads from the unified config's `permissions` section
      (`buildPolicy` → `config.Decode("permissions", …)`), merged with the legacy
      layered `permissions.json` (still loaded). "Always allow this" persistence
      and atomic writes are unchanged (still append to `permissions.json`).
- [x] Pricing overrides load from the unified config's `pricing` section via
      `cost.DefaultWith` (embedded ← config section ← `$SMITH_PRICING`); the
      embedded defaults and the `$SMITH_PRICING` escape hatch still work.
- [x] `smith config show` reflects the migrated `permissions`/`pricing` sections
      with correct per-leaf sources.
- [x] No silent format break: `permissions.json` and `$SMITH_PRICING` keep
      working; the removed flat `.smith/config` is documented (ADR-0002) and a
      leftover flat file triggers a `config show` warning (PRD D2 / D0).

## Dependencies

- AS-031 (the substrate this migrates consumers onto) — provides
  `internal/config`.
