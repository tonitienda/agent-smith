---
id: AS-144
title: "auth set/status leaks a raw dbus error instead of the actionable env-var hint when no Secret Service is reachable"
status: ready-to-implement
github_issue: null
type: bug
depends_on: [AS-017]
area: faces
priority: P2
source: QA manual-test-campaign pass 2026-06-26 (step 3.7)
---

# AS-144 · Keychain-unreachable error is not classified as `ErrUnavailable`

**Status: ready-to-implement**

## Description

Found during the manual test campaign (`docs/projects/manual-test-campaign.md`
step 3.7) on a headless Linux host with no running D-Bus / Secret Service — the
exact CI/headless case AS-017 calls out as a supported configuration (keys
supplied via env vars).

AS-017's acceptance criteria promise an actionable error here:

> A missing key produces a clear, actionable error naming both `smith auth set`
> and the provider env var escape hatch. (`auth status` env hint +
> `unavailableErr`.)

and the campaign step 3.7 expects:

> On a host with no Secret Service running, `smith auth set openai` → Fails with
> an actionable error pointing at `OPENAI_API_KEY`; no plaintext file is written.

Actual behaviour on a headless Linux box (no `dbus-launch` on `PATH`):

```
$ smith auth set openai
smith: keychain set "openai": exec: "dbus-launch": executable file not found in $PATH   # exit 1

$ smith auth status
anthropic	error: keychain get "anthropic": exec: "dbus-launch": executable file not found in $PATH
openai	error: keychain get "openai": exec: "dbus-launch": executable file not found in $PATH
```

The raw error is leaked, does **not** mention `OPENAI_API_KEY` / the
`ANTHROPIC_API_KEY` escape hatch, and `auth status` shows `error: …` instead of
the `envHint("no keychain available", …)` line.

## Root cause

`internal/credential/credential.go` (`Keyring.Get/Set/Remove`) only maps
`keyring.ErrUnsupportedPlatform` to `credential.ErrUnavailable`. When the
platform *is* supported (Linux) but the Secret Service is unreachable — D-Bus
not running, `dbus-launch` missing, or `org.freedesktop.secrets` not
provided — go-keyring returns a plain `error`, which falls through to the raw
`fmt.Errorf("keychain %s %q: %w", …)` wrap. So `cmd/smith/auth.go` never reaches
its `errors.Is(err, credential.ErrUnavailable)` branch and the actionable
`unavailableErr` / `envHint` paths never fire.

## Acceptance criteria

- [ ] On a Linux host with no reachable Secret Service, `smith auth set openai`
      fails with the actionable `unavailableErr` message naming `OPENAI_API_KEY`
      (not a raw `dbus-launch` exec error).
- [ ] `smith auth status` on the same host prints the `no keychain available
      (set ANTHROPIC_API_KEY or \`smith auth set anthropic\`)` hint rather than
      `error: keychain get …`.
- [ ] `Keyring.Get/Set/Remove` classify the Secret-Service-unreachable failure
      modes (missing `dbus-launch`, D-Bus connection refused, name not provided)
      as `credential.ErrUnavailable`, while genuinely unexpected errors still
      propagate verbatim.
- [ ] No plaintext key file is written in any of these paths (AS-017 invariant).
- [ ] A test covers the unreachable-Secret-Service classification through the
      `Store` seam (no host keychain required).

## Notes

- Keep the unexpected-error path intact: only the recognized
  "secret store unreachable" signatures should downgrade to `ErrUnavailable`, so
  a real backend bug is not silently masked as "no keychain".
- The env-var resolution path (`credential.Resolve`) already treats
  `ErrUnavailable` as "absent, not fatal", so fixing the classification also
  makes a normal `smith run` on a headless box fall back cleanly to env vars
  instead of surfacing the raw dbus error.

## Dependencies

- AS-017 (the keychain storage + `auth` verbs this corrects).
