# ADR-0002 — Layered config format: JSON over YAML (AS-031)

> Status: **accepted** · Scope: the layered configuration substrate (PRD §7, Appendix C.3, Appendix D) · Date: 2026-06-17

## Context

AS-031 builds the configuration substrate the fast-follow features hang off:
provider/model defaults, permission rules (AS-016), pricing overrides (AS-020),
sub-agent toggles (PRD Appendix C.3), personality (Appendix D), MCP servers,
hooks. Until now each feature read its own file ad hoc — permissions and pricing
both ship small **JSON** files (`internal/permission`, `internal/cost`), and the
CLI has a flat `key = value` chain (`internal/cli/config.go`, D-CLI-6). This ADR
picks the on-disk format for the unified, nested, layered config.

The PRD's appendices write their config examples in **YAML** (C.3 subagents, D
personality), so YAML was the presumed format. But the repo has a standing
**stdlib-only** lean for tooling, and there is no YAML parser in the standard
library — adopting YAML means a new dependency (`gopkg.in/yaml.v3`) or
hand-rolling a parser. The config must also be **forward-compatible**: unknown
keys are preserved and tolerated, never fatal (PRD D2), and **nested** (the
appendix schemas are trees, not flat scalars), which the existing flat
`key = value` format cannot represent.

## Decision

Use **JSON** (`encoding/json`, stdlib) as the on-disk format for the layered
config, decoded into an open `map[string]any` tree.

- **No new dependency.** JSON parsing is stdlib; the existing permission and
  pricing files are already JSON, so a later consolidation (AS-071) does not
  cross a format boundary.
- **Nested + forward-compatible.** A `map[string]any` tree expresses the
  appendix schemas (`subagents.insights.enabled`, `personality.names.user`) and
  tolerates unknown keys by construction — they decode, merge, and are preserved;
  unrecognized top-level sections only produce a warning naming the file and key.
- **YAML is a superset of JSON**, so this does not foreclose accepting YAML
  later: if hand-editing friction proves to warrant it, a YAML front-end can
  decode into the same tree behind a build-tagged or optional dependency, with
  zero change to the typed accessors or precedence logic. JSON is the
  lowest-dependency way to ship the substrate now.

Layers merge lowest-to-highest precedence: **built-in defaults → env → user file
→ project file → flags** — the low→high reading of the established **D-CLI-6**
chain (`flag > project > user > env > default`). Env sits *below* the files on
purpose, so a checked-in repo config stays reproducible regardless of ambient
environment. Nested objects deep-merge key by key; scalars and lists are
replaced wholesale by the highest layer that sets them (list semantics are
*override*, not append, so precedence stays predictable). Every resolved leaf
records the `Source` (layer + origin file) that won it, which is what `smith
config show` prints.

### Alternatives considered

- **YAML (`gopkg.in/yaml.v3`)** — matches the PRD examples and is friendlier to
  hand-edit (comments, no braces), but adds a dependency for a benefit (ergonomics)
  the substrate does not need to ship. Deferred, not rejected: see the superset
  note above.
- **Keep the flat `key = value` format** (extend `internal/cli/config.go`) — zero
  new format, but it cannot represent the nested appendix schemas (subagents,
  personality, budgets), which is the whole point of the substrate.

## Consequences

- The config file is `.smith/config.json` (project) and
  `<UserConfigDir>/smith/config.json` (user) — the `.json` sibling of the flat
  `config` file that `config get`/`set` still use. The two config files coexist
  until **AS-071** consolidates the flat chain, permissions, and pricing onto
  this substrate.
- This ADR deviates from the AS-031 ticket's "YAML format" wording; the
  deviation and its rationale (stdlib-only, JSON superset) are recorded here per
  PRD D0 (no silent punts).
- Migrating the permission and pricing consumers off their own loaders onto this
  system is **out of scope** for AS-031 (lean core) and tracked as **AS-071**.
