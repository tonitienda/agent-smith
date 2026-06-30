---
id: AS-167
title: Command surface simplification and progressive disclosure audit
status: Pending Debrief
area: commands
priority: P1
depends_on: [AS-022, AS-066, AS-090, AS-104, AS-105]
source: docs/project/PRD.md, docs/project/CLI-UX.md, docs/project/project-intelligence-map-prd.md
---

# AS-167 · Command surface simplification and progressive disclosure audit

## Description

Smith has accumulated a large and growing set of slash commands and CLI verbs. The shared registry/parity work gives the product consistency, but it does not yet answer the product question of whether the command surface is too broad, too repetitive, or too intimidating for first-time users.

Run a product and UX audit of every current and planned command, then propose a simplified command architecture that preserves expert power while reducing beginner overwhelm. The goal is not to remove capability. The goal is to collapse similar verbs into clearer families, favor progressive disclosure, and make the first-use command list feel small and learnable.

Smith has not been released yet and therefore is valid making aggressive and breaking changes.

## Acceptance criteria

- [ ] Inventory every current and planned slash command / subcommand family, including interactive-only commands and deferred PRD commands.
- [ ] Classify each command as essential default, advanced-but-discoverable, alias-only, or merge/collapse candidate.
- [ ] Produce a recommended command taxonomy with a small set of memorable nouns/families and a rationale for each merge or split.
- [ ] Define how the TUI palette, `/help`, onboarding, and CLI help should expose beginner-safe commands first while keeping advanced flows reachable.
- [ ] Identify any compatibility constraints where old commands should remain as aliases because of additive-only CLI/command contract expectations.
- [ ] Spin out implementation tickets for accepted changes rather than baking design decisions into this discovery ticket.

## Debrief questions

- Which commands should remain first-class visible entry points versus hidden aliases?
- Should context-control actions (`/context`, `/clean`, `/tidy`, `/compact`, `/rewind`) collapse under fewer nouns or remain separate?
- Should project-intelligence and workboard capabilities extend existing commands first before adding `/map` or other new top-level verbs?
- What is the beginner-safe default view of `/help` and the command palette?

## Dependencies

[AS-022, AS-066, AS-090, AS-104, AS-105]
