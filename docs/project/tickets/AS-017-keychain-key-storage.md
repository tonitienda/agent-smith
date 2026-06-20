---
id: AS-017
title: OS-keychain API key storage
status: ready-to-implement
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

- [ ] Keys are never written to disk in plaintext by Agent Smith.
- [ ] `smith auth set/remove/status` manages keys per provider.
- [ ] Env vars override stored keys, enabling CI/headless use.
- [ ] A missing key produces a clear, actionable error naming both `smith auth set` and the provider env var escape hatch.
- [ ] Key lookup goes through a narrow internal interface so tests can run without the host keychain.

## Dependencies

- AS-001 (CLI entrypoint to hang `smith auth` on)
