---
id: AS-118
title: "Root help ignores --output json"
status: done
type: bug
github_issue: 383
depends_on: [AS-065, AS-070]
area: faces
priority: P2
source: Manual smoke pass for docs/projects/manual-test-campaign.md on 2026-06-22
---

# AS-118 · Root help ignores `--output json`

**Status: done** *(renumbered to AS-118 — both AS-116 and AS-117 were already taken by other tickets)*

## Description

The manual smoke pass for the comprehensive test campaign found that root help
still renders plain text when invoked with `--output json`:

```sh
./smith --help --output json
```

The output starts with the plain text banner (`Agent Smith is a provider-agnostic
coding agent harness.`), so it is not parseable JSON. This contradicts the README
claim that `smith --help`, `smith <cmd> --help`, and `smith <cmd> --help --output
json` document the command tree in machine-readable form, and it is adjacent to
AS-070's command-specific JSON help acceptance criteria.

## Acceptance criteria

- [x] `smith --help --output json` emits valid JSON and exits successfully.
- [x] The JSON includes the root command summary, global flags, and child command
      metadata needed to discover the command tree (groups expose their verbs via `sub`).
- [x] Text help remains unchanged for `smith --help`.
- [x] Leaf command JSON help remains valid and keeps command-specific flags.
- [x] Tests cover root JSON help (`TestRootHelpJSON`) and leaf command JSON help (`TestCommandHelpJSON`).

## Dependencies

- AS-065 (CLI router/help infrastructure).
- AS-070 (leaf help JSON parity precedent).
