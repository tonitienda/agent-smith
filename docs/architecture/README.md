# Architecture

This directory documents Agent Smith's software architecture using the [C4 model](https://c4model.com/) and Mermaid diagrams. Keep these files small and linked instead of turning one document into the whole architecture.

## Reading order

1. [System context](system-context.md) — C4 level 1: users and external systems around Agent Smith.
2. [Containers](containers.md) — C4 level 2: runtime containers, data stores, and external integrations.
3. [Core components](core-components.md) — C4 level 3 for the critical in-process containers that make the product thesis work.
4. [Runtime flows](runtime-flows.md) — sequence diagrams for the most important execution paths.
5. [Dependency boundaries](dependency-boundaries.md) — which layers may import third-party code, and the guard test that enforces the stdlib-first core.
6. [Package contracts](package-contracts.md) — dependency direction and ownership between core packages, where new code goes, and the guard test that enforces it.

## Scope and source of truth

The product commitments in the PRD decision log remain the source of truth for architectural tradeoffs: provider neutrality, an open additive-only schema, append-only session logs, and projection-based context are non-negotiable constraints. This architecture documentation explains the current implementation shape and should be updated whenever those implementation seams change.
