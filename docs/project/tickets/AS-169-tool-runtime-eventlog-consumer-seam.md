---
id: AS-169
title: tool.Runtime couples to concrete *eventlog.Log instead of a consumer seam (AS-091)
status: needs-clarification
github_issue: null
depends_on: [AS-091, AS-098]
area: architecture
priority: P3
source: docs/architecture/package-contracts.md; QA pass 2026-07-01
---

# AS-169 · tool.Runtime couples to concrete `*eventlog.Log` instead of a consumer seam (AS-091)

**Status: needs-clarification** *(raised during a QA pass comparing the
architecture docs, arch tests, and code)*

## Description

A 2026-07-01 QA pass found `docs/architecture/package-contracts.md` claimed the
tools layer depends only on `schema` ("depending only on `schema` and stdlib").
That claim was inaccurate and the doc has been corrected in the same `[QA]` PR:
the shared `internal/tool` package actually points inward at **two** lower
contracts.

- `internal/tool/runtime.go` holds a concrete `*eventlog.Log` (`NewRuntime(registry
  *Registry, log *eventlog.Log, ...)`) and records `tool_result` blocks to it
  (`eventlog.Derive`, append). Verified: `go list -f '{{.Imports}}' ./internal/tool`
  → `internal/eventlog`.
- `internal/tool/registry.go` renders tool defs into `internal/provider.ToolDef`
  (`Registry.ProviderDefs`). Verified: same command → `internal/provider`.

Neither dependency violates the *load-bearing* rule — a tool must never import
the loop or a face, and it does not (`layering_test.go` still passes; both
`eventlog` and `provider` are lower/contract layers, so these edges point
inward). The concrete built-in tools under `internal/tool/builtin` remain
`schema`-only leaves. So the corrected doc simply names the real edges.

**The open question is a consistency one, not a violation.** The loop consumes
the event log through a tiny *consumer seam* — `EventLog` (`Append`/`Events`) —
per the AS-091 "accept interfaces, return concrete structs" convention documented
in `package-contracts.md`. The tool `Runtime` instead takes the concrete
`*eventlog.Log`. Should the `Runtime` be migrated onto a small recorder seam (the
one or two methods it actually calls) so the tool layer is not concretely coupled
to `eventlog`, matching the loop and the stated convention? Or is the concrete
type the right call here (the runtime genuinely needs `eventlog.Derive` +
provenance, and Classical testing prefers the real collaborator when it is cheap
and deterministic — the tool tests already drive a real `*eventlog.Log`)?

## Open questions

1. **Seam or concrete?** Introduce a consumer-side `interface{ Append(...);
   Derive(...) }` (name TBD) at `internal/tool`, satisfied by `*eventlog.Log`, and
   have `NewRuntime` accept it — or keep the concrete dependency and treat the
   AS-091 section as "product boundaries + the loop's seams", not a blanket rule
   every consumer must follow?
2. **Provider def rendering.** `Registry.ProviderDefs` couples `tool` to
   `provider`. Is rendering the wire format the tool layer's job (current), or
   should the loop/composition root own that conversion so `tool` needs only
   `schema`? This is the same accept-interfaces question one layer over.
3. **Guard.** If a seam is adopted, add a `layering_test.go` case pinning
   `internal/tool`'s allowed inward imports so the concrete `eventlog`/`provider`
   dependency cannot silently return.

## Notes

- No functional bug. This is purely an interface-convention consistency question
  surfaced by correcting a stale dependency claim in the architecture map.
- The doc claim itself is already fixed in the QA PR that filed this ticket
  (`package-contracts.md` tools row + "A new tool" narrative now name the real
  `eventlog`/`provider` edges); this ticket only tracks the code-shape decision.
