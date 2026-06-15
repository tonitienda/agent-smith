---
id: AS-066
title: Shared command registry — slash ↔ subcommand parity metadata
status: ready-to-implement
github_issue: null
depends_on: [AS-022, AS-065]
area: commands
priority: P1
source: AS-022 follow-on; CLI-UX.md (D-CLI-10), UX.md §9.3/§17.5
---

# AS-066 · Shared command registry — slash ↔ subcommand parity metadata

**Status: ready to implement**

## Description

Spun out of AS-022 (slash-command framework, `done`). AS-022 built the registry
for the TUI palette; the CLI router (AS-065) now consumes the same registry for
subcommands (D-CLI-10). This ticket adds the **parity metadata** so one command
definition can serve both faces honestly and a parity table can be generated
rather than hand-maintained (UX.md §17.5).

Per UX.md §9.3 each command should declare: name, aliases, summary, argument
schema, output schema where applicable, permission requirements, side effects,
and **scriptability** — interactive-only, scriptable, or both (with a reason when
interactive-only). Today's registry covers the TUI-facing subset; this fills the
scriptability/output-schema fields and exposes them to both faces.

Scope:

- Add scriptability + output-schema fields to the command descriptor.
- Backfill them for the V1 commands (`/context`↔`context show`,
  `/clean`↔`context clean`, `/cost`↔`cost`, `/clear`, `/model`, `/resume`↔
  `session resume`, etc.), including a reason on any interactive-only command.
- Generate the parity table (UX.md §17.5) from the registry so docs can't drift.
- Reconcile the legacy flag forms in AS-051/AS-064 (e.g. `smith --resume <id>`)
  to the noun-grouped form (`smith session resume <id>`) as the canonical path.

## Acceptance criteria

- [ ] Every registered command declares its scriptability (interactive-only |
  scriptable | both) and, where it emits structured output, an output schema.
- [ ] The slash form and subcommand form of a shared command come from one
  descriptor (no parallel definitions).
- [ ] A parity table is generated from the registry and matches UX.md §17.5.
- [ ] Interactive-only commands carry a stated reason; a test asserts none are
  silently interactive-only.

## Dependencies

- AS-022 (the registry this extends), AS-065 (the CLI router that consumes the
  scriptability metadata).
