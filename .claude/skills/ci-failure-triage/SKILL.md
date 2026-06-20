---
name: ci-failure-triage
description: >
  Map a failing Agent Smith CI job to the local harness command that reproduces
  it and produce the shortest reproduction plan. Use when a GitHub Actions check
  is red, a PR's CI failed, or someone asks why CI broke and how to fix it
  locally. Triggers: "CI failed", "check is red", "reproduce the CI failure",
  "why did the build break", "make CI green".
license: MIT
---

# CI failure triage

Canonical contract & parity table:
[`docs/agent-quality-gates.md`](../../../docs/agent-quality-gates.md) (CI/local
parity). Every CI job maps to one local `make` command — reproduce locally
instead of pushing speculative fixes.

## CI job → local command

| CI job (`.github/workflows/ci.yml`) | Step | Local command |
| --- | --- | --- |
| `test` (ubuntu + macos) | Build smith | `make build` |
| `test` (ubuntu + macos) | Run unit tests | `make test` |
| `test` (ubuntu + macos) | Run go vet | `make vet` |
| `lint` | Run golangci-lint | `make lint` |

The whole sequence in CI order: `scripts/harness/ci-local.sh`. Architecture
contracts, the schema guard, and the CI/local parity guard
(`internal/harnessparity`) run inside `make test`. There is no separate `fmt`
job — formatting drift surfaces as a `lint`/diff failure.

## Triage steps

1. Read the failed job's name and step to find the row above.
2. Run that single local command to reproduce (e.g. `lint` job → `make lint`).
3. If it reproduces, fix and re-run the same command, then `scripts/harness/full.sh`.
4. If it does **not** reproduce locally, suspect environment/toolchain drift
   (Go version, OS-specific test, pinned linter) — note it as an environment
   warning rather than guessing.
5. If CI gained or renamed a check, update the parity table **and** the harness
   scripts in the same change; the `harnessparity` guard fails on drift.

Report the command run, its exit status, the failure summary, and the next
command — matching the repo testing-summary convention.
