# Dependency boundaries

Agent Smith is **stdlib-first** (PRD D6, `CLAUDE.md`). External dependencies are
appropriate only at the outer faces — chiefly the terminal UI — and at the
process composition root. The architectural core stays on the Go standard
library plus this module so it remains portable, testable offline, and free of
vendor lock-in.

This boundary is enforced by a guard test, not by review: see
`internal/archtest/boundaries_test.go`. Adding a third-party import to a core
package will fail `make test`.

## Layers

| Layer | Packages | May import third-party? |
|---|---|---|
| **Executable / composition** | `cmd/...` (e.g. `cmd/smith`) | Yes — process-edge wiring (terminal setup, the TUI face). |
| **Face** | `internal/tui` | Yes — the interactive terminal UI builds on Bubble Tea / Lip Gloss / Glamour. |
| **OS secret store** | `internal/credential` | Yes — the OS-keychain adapter wraps `go-keyring` (macOS Keychain / Linux Secret Service / Windows Credential Manager); there is no stdlib equivalent (AS-017, D9). |
| **Core** | everything else: `schema`, `internal/eventlog`, `internal/projection`, `internal/provider` (+ adapters), `internal/loop`, `internal/cost`, `internal/budget`, `internal/config`, `internal/permission`, `internal/tool` (+ `builtin`), `internal/command`, capability packages (`memory`, `skill`, `customcmd`, `hook`, `mcp`, `subagent`), `internal/compact`, `internal/clean`, `internal/rewind`, `internal/snapshot`, `internal/session`, `internal/smithapp`, `internal/cli`, … | **No** — Go standard library and this module only. |

The **provider adapter** packages (`internal/provider/anthropic`,
`internal/provider/openai`) are core: they speak HTTPS with `net/http` and the
stdlib JSON/SSE machinery, never a vendor SDK.

## Allowed exceptions

Beyond the face (`internal/tui`) and composition root (`cmd/smith`) — which
depend on the Charmbracelet TUI stack and `golang.org/x/term` — one core-adjacent
adapter imports a third-party library:

Any exception must be justified by a ticket, documented in this table, and added
to the allow-list in `internal/archtest/boundaries_test.go` — the test fails
otherwise, so the documentation and the enforcement cannot silently drift apart.

| Exception | Package | Justification |
|---|---|---|
| `go-keyring` | `internal/credential` | OS-keychain key storage has no stdlib equivalent; the package is a thin adapter behind the `credential.Store` seam so the rest of core depends only on the interface (AS-017, PRD D9). |
