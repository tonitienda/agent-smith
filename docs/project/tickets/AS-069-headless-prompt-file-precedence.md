---
id: AS-069
title: "smith run -f <file> is shadowed by ambient stdin on a non-TTY"
status: done
github_issue: null
depends_on: [AS-065]
area: faces
priority: P2
source: AS-065 follow-on; Gemini review on PR #111
---

# AS-069 · `smith run -f <file>` is shadowed by ambient stdin on a non-TTY

**Status: done**

## Description

Spun out of a Gemini review on PR #111 (AS-066). `resolvePrompt` in
`cmd/smith/cli.go` resolves the headless prompt per D-CLI-3 precedence:
positional → piped stdin (non-TTY) → `-f <file>`. The stdin case is gated on
`!c.StdinTTY`, which is true in **any** non-interactive environment (CI, a
script, `stdin` redirected from `/dev/null`) — not only when data is actually
piped. So `smith run -f task.md` in CI hits the stdin branch first, reads an
empty stream, and fails with "empty prompt" instead of reading the file. The
`-f` flag is effectively unusable in its primary (scripted) environment.

The review's suggested fix simply reorders the switch to put `-f` above stdin,
but that **inverts the documented D-CLI-3 precedence** (piped stdin should still
beat `-f` when data is genuinely piped, e.g. `echo data | smith run -f x.md`).
The right fix honours the intent of D-CLI-3 #2 — *actual piped data* — by
distinguishing "stdin has data" from "non-TTY with nothing piped":

- When non-TTY, read stdin; if it yields a non-empty prompt, use it (stdin still
  outranks `-f`, per D-CLI-3).
- If stdin is empty **and** `-f` was given, fall back to the file rather than
  erroring.
- Preserve the explicit `-` and positional cases unchanged.

## Acceptance criteria

- [x] `smith run -f task.md` reads the file on a non-TTY with no piped stdin.
- [x] `echo data | smith run -f task.md` still prefers the piped stdin (D-CLI-3).
- [x] Positional, explicit `-`, and the no-prompt usage error are unchanged.
- [x] A test covers the non-TTY `-f` case and the piped-stdin-beats-`-f` case.

## Dependencies

- AS-065 (the CLI router and `resolvePrompt` this fixes).
