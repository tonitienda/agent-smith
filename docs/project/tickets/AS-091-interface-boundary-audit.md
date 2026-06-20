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

- [x] Existing interfaces are classified as product boundary, consumer seam, or
      unnecessary abstraction. See
      [docs/architecture/interface-conventions.md](../../architecture/interface-conventions.md).
- [x] At least three non-product seams are shrunk, moved to the consumer package,
      or replaced with concrete structs/functions. The duplicated config-reader
      seam was unified to the AS-093 `configReader` name in `internal/hook` and
      `internal/subagent` (it had drifted to `configDecoder`), and the
      hand-written `Decode` test doubles for that seam were removed in favour of
      the real `*config.Config` collaborator.
- [x] Tests use smaller fakes or concrete helpers after the migration: the
      `fakeDecoder` doubles in hook/subagent tests are gone, replaced by a real
      config built from a temp JSON file.
- [x] A short docs note records the interface convention for future agents:
      [docs/architecture/interface-conventions.md](../../architecture/interface-conventions.md),
      linked from the architecture README and package-contracts.
- [x] Test updates restructure the affected hook/subagent config-load tests to
      the Classical strategy: real in-process config collaborator over a guessed
      double, deterministic and offline.

## Dependencies

- None
