---
id: AS-147
title: Sandbox provider interface and disposable microVM prototype
status: ready-to-implement
area: cloud-security
priority: P2
depends_on: [AS-144, AS-146]
source: docs/projects/smith-cloud-prd.md
---

# AS-147 · Sandbox provider interface and disposable microVM prototype

## Description

Create the sandbox abstraction and first dogfood backend for short-lived isolated subtasks, targeting Hetzner VPC plus Firecracker or an equivalent microVM/container backend behind the same interface.

## Acceptance criteria

- [ ] Interface covers image selection, repo checkout workspace, resource limits, egress policy, secret mount/injection hooks, artifact extraction, snapshot/teardown, and telemetry.
- [ ] Prototype can boot a disposable environment, run a no-op Smith task, capture logs/artifacts, and destroy the workspace.
- [ ] Security documentation states what isolation is provided by the backend and what remains a known compromise.

## Dependencies

[AS-144, AS-146]
