---
id: AS-153
title: Sandbox abstraction and execution environments
status: needs-clarification
area: security
priority: P2
depends_on: [AS-144, AS-146, AS-158]
source: docs/projects/smith-orchestrator-dogfood-prd.md
---

# AS-153 · Sandbox abstraction and execution environments

## Description

Design the execution-environment seam for orchestrated work, starting from local/VPC dogfood and preserving a path to rootless containers or microVMs for disposable workers.

## Acceptance criteria

- [ ] Interface covers image/profile selection, repo checkout, workspace lifecycle, resource limits, egress policy, artifact extraction, teardown, and telemetry.
- [ ] The design separates no-sandbox/local checkout, private VPC runner, rootless container, and microVM implementations.
- [ ] MVP 0 can proceed without pretending local execution is a security sandbox.
- [ ] Later worker protocol needs are captured without blocking GitHub-triggered dogfood automation.
- [ ] Documentation states which isolation guarantees each backend does and does not provide.

## Dependencies

[AS-144, AS-146, AS-158]
