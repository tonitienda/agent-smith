---
id: AS-112
title: Guard the declarative-only plugin boundary with a test + archtest
status: done
github_issue: 378
depends_on: [AS-044, AS-098]
area: quality
priority: P1
source: docs/design/plugin-trust.md §4.3; spun out of AS-059
---

# AS-112 · Guard the declarative-only plugin boundary

**Status: ready to implement** *(spun out of the AS-059 plugin-trust spike)*

## Description

The D9 declarative-only boundary (a third-party plugin is data, never code) is
currently enforced *structurally* — `LoadManifest` wraps a manifest in a
`declarative` sub-agent whose lifecycle methods are no-ops — but only **documented
by a comment**. AS-059 §4.3 calls for the boundary to be guarded by a test so a
future refactor cannot quietly grow a code-execution path from a parsed manifest.

Two guards:

1. **Behavioral unit test:** loading *any* manifest via `LoadManifest` yields a
   sub-agent whose `Init/Observe/Teardown` produce no findings and zero spend.
   **This guard is valid only while declarative plugins are entirely non-functional
   (the v1 line).** When a framework-side model-execution path for declarative
   plugins lands (running a plugin's prompt on the user's behalf), these sub-agents
   *will* emit findings and incur spend — so this assertion must then be
   re-parameterized (e.g. "no *arbitrary third-party code* runs; spend is bounded by
   the budget cap") rather than "zero spend". The test comment must say so, so the
   guard is updated deliberately, not deleted in confusion.
2. **Architecture assertion (`internal/archtest`):** the declarative third-party
   path imports no `os/exec`, no `net/http`, no filesystem-write surface — there is
   no edge from a parsed third-party manifest to arbitrary execution or egress.

## Acceptance criteria

- [x] A test asserts `LoadManifest`'d sub-agents are no-op / zero-spend across a
      table of valid manifests. (`TestDeclarativeBoundaryNoOp`, `internal/subagent`)
- [x] An `archtest` assertion forbids `os/exec` and `net/http` on the declarative
      plugin path. (`TestDeclarativePluginBoundaryHasNoExecOrEgress`, `internal/archtest`)
- [x] The guards run in the standard `make test` / `arch` harness.

## Dependencies

- AS-044 (the registry), AS-098 (architecture-contract test substrate).

