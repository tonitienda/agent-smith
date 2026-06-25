---
id: AS-149
title: GitHub App integration and repository permission model
status: ready-to-implement
area: integrations
priority: P2
depends_on: [AS-144]
source: docs/projects/smith-cloud-prd.md
---

# AS-149 · GitHub App integration and repository permission model

## Description

Register and integrate a Smith GitHub App so users/orgs grant repository access without PATs.

## Acceptance criteria

- [ ] Design lists required GitHub App permissions for PR read/write, contents, issues, checks/statuses, and webhook events, each justified by a product flow.
- [ ] Installation tokens are minted per repository/job and expire quickly.
- [ ] Webhook ingestion verifies signatures and normalizes pull_request.closed/merged, issue/comment command, and manual dispatch events into cloud triggers.
- [ ] Repo allowlists and org policies can prevent jobs from running on unapproved repositories.

## Dependencies

[AS-144]
