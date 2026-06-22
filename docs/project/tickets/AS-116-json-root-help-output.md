---
id: AS-116
title: "Root help ignores --output json"
status: ready-to-implement
type: bug
github_issue: null
depends_on: [AS-065, AS-070]
area: faces
priority: P2
source: Manual smoke pass for docs/projects/manual-test-campaign.md on 2026-06-22
---

# AS-116 · Root help ignores `--output json`

**Status: ready to implement**

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

- [ ] `smith --help --output json` emits valid JSON and exits successfully.
- [ ] The JSON includes the root command summary, global flags, and child command
      metadata needed to discover the command tree.
- [ ] Text help remains unchanged for `smith --help`.
- [ ] Leaf command JSON help remains valid and keeps command-specific flags.
- [ ] Tests cover root JSON help and at least one leaf command JSON help.

## Dependencies

- AS-065 (CLI router/help infrastructure).
- AS-070 (leaf help JSON parity precedent).
