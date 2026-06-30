---
id: AS-153
title: Sandbox abstraction and execution environments
status: needs-clarification
area: security
priority: P2
depends_on: [AS-159, AS-161, AS-158]
source: docs/project/smith-orchestrator-dogfood-prd.md
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

## Research input (AS-158)

See [orchestrator-competitive-research.md §3 AS-153](../../research/orchestrator-competitive-research.md#as-153--sandbox-abstraction-and-execution-environments):
backends weakest→strongest (local checkout / VPC runner / rootless container /
microVM) with per-backend isolation guarantees documented (Ona model); adopt a
two-phase lifecycle (network-on setup, default-deny-egress agent phase — Codex)
with allowlist + clear deny errors (Claude `403 host_not_allowed`); MVP 0 ships
the local backend only behind the interface with no security claims.

## Dependencies

[AS-159, AS-161, AS-158]
