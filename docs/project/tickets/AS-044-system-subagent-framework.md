---
id: AS-044
title: System sub-agent lifecycle framework + plugin registry
status: done
github_issue: 44
depends_on: [AS-006, AS-018, AS-020, AS-031]
area: subagents
priority: P1
source: PRD.md §7.19, §5, Appendix C.3–C.5, D9
---

# AS-044 · System sub-agent framework + plugin registry

**Status: done** — shipped in `internal/subagent` (manifest + registry +
lifecycle Runner + in-memory insights Store), a self-contained, face-agnostic
framework. Observe is passive by construction (no model calls, no I/O); teardown
runs at the scope boundary off the hot path; findings live in the Store, never on
the event log, so a disabled analyzer adds zero blocks and zero tokens. Per-sub-
agent budget caps reuse the AS-041 `budget.Guard`. Wiring the Runner into the
turn loop (init at span start, observe per appended block, end at span/session
teardown) is the consumer step tracked as **AS-088**; the substrate lands first,
the same way `budget.Guard` landed before the loop opted it in.

## Description

The new primitive (§7.19) that powers most wedges: built-in specialized sub-agents driven at lifecycle hooks, with the contract **init(scope) → observe (passive, no model calls) → teardown(scope, state)** (Appendix C.4). Built-ins are *first-party plugins* on a public interface — registry + manifest from day one (C.5).

- Lifecycle engine: the main agent invokes init at span start, observe accumulates trace signals from the log (passively — by construction no model calls), teardown hands over the relevant context slice for analysis; findings report into the insights store.
- Manifest (C.5): name, kind, hooks, scope, model tier, enabled_by_default, budget, emits, permissions (`read_transcript`, `propose_edit` — propose-only, never writes without confirm).
- Config (C.3): per-sub-agent enable/model/schedule (`teardown | session_end | rollup`)/mode/budget.
- Scheduling: async/batched at teardown or session end — never inline with an interactive turn; budget-capped via the AS-041 enforcement API.
- Third-party loading: declarative-only manifests (D9 — no arbitrary code) through the same registry as built-ins. Trust/sandboxing depth is AS-059's question; this ticket enforces the declarative boundary.

## Acceptance criteria (PRD §7.19 AC, verbatim where possible)

- [x] Enabling/disabling a sub-agent is one config line. (`subagents.<name>.enabled`, `TestConfigEnableDisable`)
- [x] A disabled analyzer adds **zero** token cost (test-enforced: no model calls, no extra blocks). Disabled agents are never inited/observed/torn down; findings live in the in-memory Store, never on the log (`TestConfigEnableDisable`).
- [x] An enabled one runs without slowing the interactive turn — observe is the only per-block work and is passive; teardown runs at the scope boundary (`TestLifecycleOrder`).
- [x] A third-party declarative manifest loads through the same registry as built-ins, as data only (`LoadManifest` → passive `declarative` wrapper, `TestLoadManifestDeclarative`).
- [x] Budget caps are enforced per sub-agent per session via `budget.Guard` over a per-sub-agent spend ledger (`TestBudgetCapEnforced`).

## Dependencies

- AS-006 (context slices), AS-018 (lifecycle hook points), AS-020/AS-041 (cost + budget enforcement), AS-031 (config)
