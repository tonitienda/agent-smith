---
id: AS-091
title: Audit interfaces and move small seams to consumer packages
status: done
github_issue: 161
depends_on: []
area: architecture
priority: P2
source: code-improvements.md
---

# AS-091 · Audit interfaces and move small seams to consumer packages

**Status: ready to implement**

## Description

The project has a few central product interfaces that are intentional, including
provider streams and client-side tools. Other seams should follow the Go rule of
thumb: accept interfaces at the consumer side and return concrete structs from
constructors.

Audit exported interfaces, callback types, and broad concrete dependencies in
observer, permission, budget, config, command, hook, loop, and related packages.
Keep central product boundaries where they are justified. For local needs,
define tiny consumer-side interfaces such as "append a block" or "read this
config value" near the package that consumes them.

## Acceptance criteria

- [ ] Existing interfaces are classified as product boundary, consumer seam, or
      unnecessary abstraction.
- [ ] At least three non-product seams are shrunk, moved to the consumer package,
      or replaced with concrete structs/functions.
- [ ] Tests use smaller fakes or concrete helpers after the migration.
- [ ] A short docs note records the interface convention for future agents.
- [ ] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure.

## Dependencies

- None

## Resolution

Audited every interface in the observer/permission/budget/config/command/hook/
loop packages (and neighbours). Classification recorded as the **Interface
convention** section in
[docs/architecture/package-contracts.md](../../architecture/package-contracts.md):
product boundaries (`provider.Provider`, `provider.Stream`, `tool.Tool`,
`permission.Asker`, `loop.Observer`, `subagent.SubAgent`, `subagent.Store`) stay;
the `configReader`/`configDecoder` views over `*config.Config` are already
correctly placed consumer seams (AS-093).

Three non-product seams shrunk, all in `internal/loop`: the `Engine` previously
accepted the whole `*eventlog.Log`, `*tool.Runtime`, and `*tool.Registry` but used
only `Append`/`Events`, `ExecuteBatch`, and `ProviderDefs` respectively. Replaced
with consumer-side `eventLog`, `toolExecutor`, and `toolDefs` interfaces declared
in the loop; constructor and field types now name just those methods (callers pass
the same concrete types, so no call sites changed). The loop's tests keep their
real collaborators per the Classical strategy — the narrowed seams simply make a
one-/two-method fake possible where a future test wants one.
