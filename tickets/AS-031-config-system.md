---
id: AS-031
title: Layered configuration system
status: ready-to-implement
github_issue: null
depends_on: [AS-001]
area: foundation
priority: P0
source: PRD.md §7 (implied throughout), Appendix C.3, Appendix D
---

# AS-031 · Layered configuration system

**Status: ready to implement**

## Description

The config substrate nearly every fast-follow feature hangs off: permission rules (AS-016), pricing overrides (AS-020), provider/model defaults, sub-agent toggles (Appendix C.3), personality (Appendix D), MCP servers, hooks. Until now config bits were specified per-ticket; this consolidates them into one layered system.

- Layers, lowest to highest precedence: built-in defaults → user (`~/.config/agent-smith/config.yaml`) → project (`.agent-smith/config.yaml`) → CLI flags/env.
- YAML format (the PRD's appendices already speak YAML); short ADR documenting the choice.
- Unknown keys warn, never crash (same forward-compatibility ethos as the schema, D2).
- `smith config` prints the effective merged config with the source layer of each value.
- Typed accessors so features don't parse YAML ad hoc.

## Acceptance criteria

- [ ] Precedence order is correct and covered by tests for scalar, map, and list merge cases.
- [ ] Unknown keys produce a warning naming the file and key, and are preserved.
- [ ] `smith config` shows effective values and where each came from.
- [ ] Existing config consumers (permissions, pricing) migrate onto this system.

## Dependencies

- AS-001 (scaffolding)
