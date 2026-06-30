---
id: AS-156
title: Private VPC deployment
status: needs-clarification
area: deployment
priority: P2
depends_on: [AS-161, AS-148, AS-154]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-156 · Private VPC deployment

## Description

Define the first always-on private deployment for the Smith orchestrator so dogfood jobs can run without the maintainer laptop staying open 24/7.

## Acceptance criteria

- [ ] Deployment design covers a single-tenant private VPC/Hetzner host, persistent run DB, logs, backups, health checks, and restart behavior.
- [ ] Webhook delivery path is documented, including local tunnel versus public endpoint trade-offs.
- [ ] Secrets and GitHub credentials follow the contract selected by AS-154.
- [ ] Runbooks explain deploy, upgrade, rollback, pause all jobs, inspect failures, rotate credentials, and restore DB backup.
- [ ] The design keeps hosted/multi-tenant assumptions out of MVP 1 while preserving future seams.

## Research input (AS-158)

See [orchestrator-competitive-research.md §3 AS-156](../../research/orchestrator-competitive-research.md#as-156--private-vpc-deployment):
single-tenant VM-per-agent runner inside the maintainer's VPC (Ona AWS-runner
shape: always-on daemon host + ephemeral run workspaces, egress-controlled);
webhook delivery via tunnel (smee/cloudflared) vs public endpoint trade-off;
encrypt the run DB at rest (AES-256-GCM precedent); keep multi-tenant
assumptions out (D9). This VPC host is the first real sandbox backend for AS-153.

## Dependencies

[AS-161, AS-148, AS-154]
