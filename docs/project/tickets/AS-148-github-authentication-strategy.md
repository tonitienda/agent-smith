---
id: AS-148
title: GitHub authentication strategy
status: done
area: integrations
priority: P2
depends_on: [AS-159, AS-147]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-148 · GitHub authentication strategy

## Description

Decide and document how the first dogfood orchestrator receives limited GitHub access, including whether MVP 0 starts with a maintainer-provided credential or requires a GitHub App from the beginning.

## Acceptance criteria

- [x] Decision record compares GitHub App installation access, fine-grained user access, and local `gh` delegation for private dogfood use.
- [x] Required access is listed per flow: read issues/PRs, read checks, create branches, push contents, open/update PRs, comment, label, and merge/auto-merge.
- [x] Permission failures are designed as clear user/operator actions rather than agent decisions.
- [x] Credential lifetime, storage location, rotation expectations, and audit behavior are documented.
- [x] Migration path from MVP dogfood auth to future GitHub App onboarding is documented.

## Resolution (2026-07-01)

Decided in [ADR-0003 — GitHub authentication strategy](../../design/adr-0003-github-auth-strategy.md):
MVP 0 = tightly scoped fine-grained maintainer PAT (Contents/PRs/Issues r/w,
Checks read), credential resolved by scope name behind an accessor seam, push
restricted to the run's own branch, fail-closed on missing scope; GitHub App
minting short-lived per-operation installation tokens is the MVP 1+ migration
target (source swap behind the same seam). Resolves Q-148.

## Clarification (resolved 2026-06-30) — research input (AS-158)

See [orchestrator-competitive-research.md §3 AS-148](../../research/orchestrator-competitive-research.md#as-148--github-authentication-strategy):
MVP 0 = tightly scoped fine-grained maintainer token (Contents/PRs/Issues r/w,
Checks read); migrate to a GitHub App minting short-lived, per-operation,
repo-scoped installation tokens (Codex/Claude/Devin model); keep the real
credential in a proxy outside the runner and restrict push to the run's own
branch; permission failures are explicit operator actions, fail closed.

## Dependencies

[AS-159, AS-147]
