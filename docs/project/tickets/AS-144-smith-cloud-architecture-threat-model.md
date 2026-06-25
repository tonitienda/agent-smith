---
id: AS-144
title: Smith Cloud architecture and threat-model spike
status: needs-clarification
area: cloud
priority: P2
depends_on: []
source: docs/projects/smith-cloud-prd.md
---

# AS-144 · Smith Cloud architecture and threat-model spike

## Description

Define the control-plane boundaries, tenant model, data flows, sandbox trust boundaries, and how this cloud layer coexists with D8/D9.

## Acceptance criteria

- [ ] Architecture decision record covers control plane, worker pool, sandbox hosts, secret store, GitHub App, event-log ingestion, and explicit out-of-scope risks.
- [ ] Threat model includes scheduler abuse, prompt injection into repo automation, secret exfiltration, sandbox escape, GitHub token misuse, noisy-neighbor resource exhaustion, artifact leakage, and run replay privacy.
- [ ] The spike recommends first dogfood deployment boundaries and updates architecture docs if a concrete runtime seam is chosen.

## Dependencies

[]

## Open questions

1. Should the first control plane live in this repository or a separate Smith Cloud service repository?
2. Should the first sandbox backend be Firecracker on Hetzner immediately, or a simpler rootless-container implementation behind the same interface for faster product validation?
3. Which secret-store backend should dogfood use first, and what migration path preserves the same job/spec contract?
4. What minimum approval signal is required before any Smith-authored PR can auto-merge?
