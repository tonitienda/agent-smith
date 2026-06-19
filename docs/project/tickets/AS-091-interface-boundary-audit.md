---
id: AS-091
title: Audit interfaces and move small seams to consumer packages
status: ready-to-implement
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

## Dependencies

- None
