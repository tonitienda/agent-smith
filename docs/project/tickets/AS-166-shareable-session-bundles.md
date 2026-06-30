---
id: AS-166
title: Shareable redacted session bundles
status: ready-to-implement
area: collaboration
priority: P1
depends_on: [AS-005, AS-055, AS-079, AS-115, AS-154]
source: docs/project/competitors.md
---

# AS-166 · Shareable redacted session bundles

## Description

Create a safe, local-first export format for sharing a Smith session with a teammate or maintainer. The bundle should preserve enough event-log detail to debug agent behavior while applying redaction, provenance, and optional expiry metadata.

## Acceptance criteria

- [ ] A user can export a read-only session bundle from the CLI/TUI.
- [ ] The export runs redaction and produces a manifest of removed or transformed fields.
- [ ] The bundle opens in the WASM/static inspector without requiring access to the original repository.
- [ ] The bundle includes schema version, Smith version, creation time, source commit, and optional expiry/retention metadata.
- [ ] The export command warns when unredacted secrets or oversized artifacts may remain.

## Dependencies

[AS-005, AS-055, AS-079, AS-115, AS-154]
