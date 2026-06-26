---
id: AS-148
title: GitHub authentication strategy
status: needs-clarification
area: integrations
priority: P2
depends_on: [AS-144, AS-147]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-148 · GitHub authentication strategy

## Description

Decide and document how the first dogfood orchestrator receives limited GitHub access, including whether MVP 0 starts with a maintainer-provided credential or requires a GitHub App from the beginning.

## Acceptance criteria

- [ ] Decision record compares GitHub App installation access, fine-grained user access, and local `gh` delegation for private dogfood use.
- [ ] Required access is listed per flow: read issues/PRs, read checks, create branches, push contents, open/update PRs, comment, label, and merge/auto-merge.
- [ ] Permission failures are designed as clear user/operator actions rather than agent decisions.
- [ ] Credential lifetime, storage location, rotation expectations, and audit behavior are documented.
- [ ] Migration path from MVP dogfood auth to future GitHub App onboarding is documented.

## Dependencies

[AS-144, AS-147]
