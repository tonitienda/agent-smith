---
id: AS-031
title: Layered configuration system
status: done
github_issue: 31
depends_on: [AS-001]
area: foundation
priority: P0
source: PRD.md §7 (implied throughout), Appendix C.3, Appendix D
---

# AS-031 · Layered configuration system

**Status: done** — core landed; consumer migration split into **AS-071**.

> **Format note:** the substrate ships as **JSON** (stdlib), not YAML, to avoid a
> dependency while staying nested and forward-compatible. YAML is a superset and
> can be added later behind the same accessors if hand-edit friction warrants it.
> See [adr-0002](../../design/adr-0002-config-format.md). The migration of
> existing consumers (permissions, pricing, the flat `config get`/`set` chain)
> was intentionally split out to keep this PR lean — tracked as **AS-071**.

## Description

The config substrate nearly every fast-follow feature hangs off: permission rules (AS-016), pricing overrides (AS-020), provider/model defaults, sub-agent toggles (Appendix C.3), personality (Appendix D), MCP servers, hooks. Until now config bits were specified per-ticket; this consolidates them into one layered system.

- Layers, lowest to highest precedence: built-in defaults → user (`~/.config/agent-smith/config.yaml`) → project (`.agent-smith/config.yaml`) → CLI flags/env.
- YAML format (the PRD's appendices already speak YAML); short ADR documenting the choice.
- Unknown keys warn, never crash (same forward-compatibility ethos as the schema, D2).
- `smith config` prints the effective merged config with the source layer of each value.
- Typed accessors so features don't parse YAML ad hoc.

## Acceptance criteria

- [x] Precedence order is correct and covered by tests for scalar, map, and list merge cases.
- [x] Unknown keys produce a warning naming the file and key, and are preserved.
- [x] `smith config` shows effective values and where each came from (`smith config show`).
- [ ] Existing config consumers (permissions, pricing) migrate onto this system. → **moved to AS-071** (kept this PR lean per the build-order decision).

## Dependencies

- AS-001 (scaffolding)
