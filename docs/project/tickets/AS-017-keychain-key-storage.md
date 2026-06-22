---
id: AS-017
title: OS-keychain API key storage
status: done
github_issue: 17
depends_on: [AS-001]
area: security
priority: P0
source: PRD.md D9
---

# AS-017 · OS-keychain API key storage

**Status: ready to implement**

## Description

D9 commits to OS-keychain storage for provider API keys in V1. Store Anthropic/OpenAI (and compatible-endpoint) keys in the operating system's secret store rather than plaintext config.

Proposed approach (pending the clarifications below): use a cross-platform keyring library (e.g., `zalando/go-keyring`) targeting macOS Keychain and Linux Secret Service; environment variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) always override as the escape hatch; a `smith auth` command to set/remove keys.

## Clarified implementation decisions

- **V1 platform scope:** support macOS Keychain, Linux Secret Service, and Windows Credential Manager through one small keyring adapter. CI/headless environments are supported through environment variables rather than requiring a desktop keychain.
- **Fallback:** Agent Smith must never create a plaintext key file. If no keychain is available, `smith auth set` fails with an actionable message and instructs the user to use `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` for that process. Environment variables always override stored credentials.
- **Profiles:** V1 stores one global key per provider plus optional endpoint-specific entries for OpenAI-compatible endpoints. Work/personal profiles are deferred until the layered config has a concrete profile concept for credentials.
- **Namespacing:** keychain service name is `agent-smith`; account names are stable provider IDs (`anthropic`, `openai`, and `openai-compatible:<config-name>`).

## Acceptance criteria

- [x] Keys are never written to disk in plaintext by Agent Smith. (Only persistent store is the OS keychain via `internal/credential.Keyring`; no plaintext fallback — `auth set` fails with `ErrUnavailable` instead.)
- [x] `smith auth set/remove/status` manages keys per provider. (`cmd/smith/auth.go`)
- [x] Env vars override stored keys, enabling CI/headless use. (`credential.Resolve` checks the env var before the keychain.)
- [x] A missing key produces a clear, actionable error naming both `smith auth set` and the provider env var escape hatch. (`auth status` env hint + `unavailableErr`.)
- [x] Key lookup goes through a narrow internal interface so tests can run without the host keychain. (`credential.Store`; tests use in-memory fakes — `internal/credential/credential_test.go`, `cmd/smith/auth_test.go`.)

## Implementation notes

- `internal/credential` is the narrow seam: `Store` (Get/Set/Remove), the
  `Keyring` OS-backed implementation (go-keyring), `Provider` account↔env-var
  mapping, and `Resolve` (env-over-keychain precedence). It is an allow-listed
  third-party exception in `docs/architecture/dependency-boundaries.md`.
- `smithapp.DefaultProviders` resolves each provider's key through `credential`
  before constructing the provider; a missing key stays empty and surfaces as
  `ErrAuth` only when a turn runs.
- Profiles and richer endpoint-specific entries beyond
  `openai-compatible:<name>` are deferred per the clarified scope above.

## Dependencies

- AS-001 (CLI entrypoint to hang `smith auth` on)
