---
id: AS-150
title: PR automation and guarded auto-merge policy
status: ready-to-implement
area: integrations
priority: P2
depends_on: [AS-149, AS-151]
source: docs/projects/smith-cloud-prd.md
---

# AS-150 · PR automation and guarded auto-merge policy

## Description

Let Smith Cloud create/update PRs and optionally request or enable auto-merge only under explicit repository policy.

## Acceptance criteria

- [ ] Sandbox output can create a branch/PR, update a prior Smith-authored PR, and post a run summary with links to session artifacts.
- [ ] Auto-merge is disabled by default and can only be enabled when job policy, repository branch protection, required checks, and reviewer/label rules allow it.
- [ ] The implementation never force-pushes protected branches, bypasses branch protections, or merges on failed/unknown checks.
- [ ] Every PR action is written to the run event log.

## Dependencies

[AS-149, AS-151]
