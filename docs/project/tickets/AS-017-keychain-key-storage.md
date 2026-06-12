---
id: AS-017
title: OS-keychain API key storage
status: needs-clarification
github_issue: 17
depends_on: [AS-001]
area: security
priority: P0
source: PRD.md D9
---

# AS-017 · OS-keychain API key storage

**Status: needs clarification**

## Description

D9 commits to OS-keychain storage for provider API keys in V1. Store Anthropic/OpenAI (and compatible-endpoint) keys in the operating system's secret store rather than plaintext config.

Proposed approach (pending the clarifications below): use a cross-platform keyring library (e.g., `zalando/go-keyring`) targeting macOS Keychain and Linux Secret Service; environment variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) always override as the escape hatch; a `smith auth` command to set/remove keys.

## Open questions (why this needs clarification)

1. **Platform scope for V1** — macOS + Linux only, or is Windows in scope? (PRD never states supported platforms; CI in AS-001 currently assumes macOS/Linux.)
2. **Fallback when no keychain is available** (headless Linux, CI, containers): env var only, or also an encrypted file? Plaintext file ever acceptable with a warning?
3. **Multiple keys per provider** (work/personal profiles): in scope for V1 or single key per provider?
4. **Key naming/namespacing** in the keychain — per profile, per project, or global?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] Keys are never written to disk in plaintext by Agent Smith.
- [ ] `smith auth set/remove/status` manages keys per provider.
- [ ] Env vars override stored keys, enabling CI/headless use.
- [ ] A missing key produces a clear, actionable error naming the fix.

## Dependencies

- AS-001 (CLI entrypoint to hang `smith auth` on)
