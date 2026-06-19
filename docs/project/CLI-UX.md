# Agent Smith — CLI UX

> Status: **conclusions from a design grilling (2026-06-15)**
> Scope: the command-line contract for both faces — the **headless CLI** (scripts/CI)
> and the **TUI launcher** — read against [docs/UX.md](../UX.md), [PRD.md](PRD.md),
> [clig.dev](https://clig.dev/), and [Bubble Tea](https://github.com/charmbracelet/bubbletea).
> Owner: Toni

This document records the decisions for how `smith` behaves *at the command line*.
[docs/UX.md](../UX.md) owns the interactive TUI experience; this owns invocation,
arguments, output, exit codes, config, and the shared command spine that both faces
render. Where it conflicts with UX.md §3.2's flag-driven sketch, **this document
wins** (see D-CLI-1).

It complements the locked UX decisions (UX.md §22) and the PRD Decision Log
(D0–D9); it does not override them. Additive-only schema discipline (D2) applies
to the CLI contract too: flags, subcommands, output fields, and exit codes are
**added, never removed or repurposed**.

---

## Decision Log — CLI (D-CLI-N)

**D-CLI-1 · Subcommand-first, noun-grouped.** The headless CLI is a git/kubectl-style
verb tree, not a single flag-driven command. Nouns group verbs as the surface
grows: `smith run`, `smith session list|resume`, `smith context show|clean`,
`smith config get|set`, `smith cost`. This supersedes UX.md §3.2's `smith -p "…"`
sketch as the *primary* shape. clig.dev: subcommands aid discoverability and scale.

**D-CLI-2 · Bare `smith` launches the TUI (on a TTY).** With no arguments and an
interactive terminal, `smith` drops straight into the flagship TUI — the zero-friction
hot path. With no TTY (piped/CI) and no subcommand, it prints help to stderr and
exits non-zero (usage error). The TUI is also reachable explicitly via `smith tui`.

**D-CLI-3 · No `-p`. Prompt is positional / stdin / file.** A task prompt enters
`smith run` three ways, in precedence order:
1. positional argument — `smith run "fix the failing test"`
2. piped stdin (when stdin is not a TTY) — `echo "fix the test" | smith run`,
   shell redirection `smith run < task.md`, or explicit `-` — `smith run -`
3. file — `smith run -f task.md` (an explicit path flag, distinct from stdin)

There is **no `-p`/`--prompt` flag** — one obvious way in. (This drops the
familiar `smith -p` from UX.md §3.2 on purpose; the positional form is shorter
anyway.)

**D-CLI-4 · Output format auto-detects the TTY.** Interactive terminal → rich
human/plain output with color. Non-TTY (piped/redirected) → plain, no ANSI.
`--output plain|json|stream-json` forces a mode regardless of TTY. `NO_COLOR` is
honored; `--color auto|always|never` overrides (default `auto`). clig.dev: be a
good Unix citizen — detect the pipe, drop the decoration. Per UX.md §2.5 / §5.2,
personality is **off** on every non-interactive path. Color is never load-bearing:
status and severity (`✓`/`✗`/`◐`, exit code, stderr) carry meaning without it
(UX.md §19 accessibility).

**D-CLI-5 · stdout is data, stderr is diagnostics.** Primary results go to stdout;
logs, spinners, progress, prompts, and errors go to stderr — always, so `| jq`
and `> file` stay clean. `--quiet`/`-q` and `--verbose`/`-v` tune stderr only.

**D-CLI-6 · Config precedence: flag > project file > user file > env > built-in
default.** A CLI flag always wins; repo-pinned config (`./.smith/…`) outranks
user config (`~/.config/smith/…`), which outranks `SMITH_*` env vars, which
outrank built-in defaults. This intentionally puts env *below* the project file
(deviating from strict 12-factor): a checked-in repo policy should be reproducible
regardless of ambient environment, while a flag covers the one-off override.
**Secrets are not part of this chain** — API keys come from the OS keychain / env
per D9 (AS-017), never from config files.

**D-CLI-7 · Exit codes: 0 / 1 / 2 now, richer table later.** V1 commits only the
universal three: `0` success, `1` runtime/task failure, `2` invalid usage
(bad flag/arg). The distinct classes UX.md §17.2 lists (permission-stop,
budget-stop, cancellation, provider-error, internal-error) are **deferred to the
headless ticket** (AS-051) and assigned additively then — codes are append-only,
so reserving them later is safe.

**D-CLI-8 · Headless never prompts; destructive ops require `--yes`.** Headless
mode never opens an interactive permission prompt (UX.md §11). Destructive context
ops (`context clean`, `clear`, `compact`, `rewind`) refuse to run on a non-TTY
without an explicit `--yes`, failing fast with a machine-readable reason. (History
is preserved by projection/event design — D3 — so this is belt-and-suspenders, not
the only safety net.)

**D-CLI-9 · Headless permission default: allowlist-then-deny.** With nothing
specified, a tool runs only if a configured allowlist permits it; otherwise it is
denied with a structured report (never a hang). `--auto` opts explicitly into
auto mode for unattended runs (Async Ana). Matches D9's "you approve actions."

**D-CLI-10 · One command registry, two faces.** TUI slash-commands and headless
subcommands resolve through the **same face-neutral command registry** (UX.md §9.3).
`/context` and `smith context show` dispatch to one handler; `/clean` and
`smith context clean` to another. A documented parity table (UX.md §17.5) states,
per command, whether it is interactive-only, scriptable, or both. Help, examples,
and `--version` ship on root and every subcommand; unknown commands get a
"did you mean…?" suggestion. Help is also machine-readable: `smith <cmd> --help
--output json` dumps the registry entry (name, aliases, args, scriptability,
output schema) straight from the face-neutral registry, so tooling and docs read
the same source the palette does. Shell completion is deferred (not V1).

---

## 1. Invocation surface (V1)

```text
smith                         # TTY → launch TUI; non-TTY → help + usage error
smith tui                     # explicit TUI launch
smith run "<prompt>"          # one task, non-interactive (positional)
echo "<prompt>" | smith run   # …or via stdin
smith run -f task.md          # …or from a file
smith run "…" --output json   # structured result
smith run "…" --output stream-json --budget 0.25 --auto
smith session list            # sessions for the project
smith session resume <id>     # resume non-interactively (TUI picker lives in AS-064)
smith context show            # composition view as data
smith context clean "<topic>" --yes   # destructive → needs --yes off-TTY
smith cost                    # token/$ accounting
smith config get|set <key> [value]
smith --version / smith --help / smith <cmd> --help
```

Global flags (apply across subcommands): `--output`, `--color`, `--quiet/-q`,
`--verbose/-v`, `--config <path>`, `--yes`. Command-specific flags (`--budget`,
`--auto`, `--model`, `-f`) live on the commands that use them.

## 2. Output modes (clig.dev + UX.md §17.1)

| Mode | When | For |
|---|---|---|
| plain (human) | TTY, default | reading; may use color (honors `NO_COLOR`/`--color`) |
| plain (bare) | non-TTY, default | pipes; no ANSI, no personality |
| `--output json` | explicit | final structured result (answer, cost, session id, stop reason) |
| `--output stream-json` | explicit | incremental events from the same substrate the TUI renders |

stream-json events are the **same events** the TUI consumes (UX.md §17.4) — one
substrate, many renderers.

## 3. Exit codes

V1: `0` success · `1` task/runtime failure · `2` invalid usage. AS-051 assigns
the richer headless taxonomy additively on top: `3` permission-stop · `4`
budget-stop · `5` cancellation · `6` provider-error (internal-error stays the
generic `1`). Codes are append-only; scripts should treat unknown nonzero codes
as failure.

## 4. How this maps to the architecture

The CLI router is a thin face over the same core UX.md §18.1 already describes:
it parses argv into a command + args, resolves the command through
`internal/command` (the face-neutral registry shared with the TUI palette),
runs it against the loop/session core, and renders the result view model
(`CommandOutputView`, etc., UX.md §18.2) as plain/JSON/stream-JSON. The TUI is
just the other renderer of that same registry. No business logic lives in the
CLI layer.

## 5. Open questions (small, non-blocking)

1. Exact noun groups beyond V1 (`smith insights`, `smith route`, `smith budget`)
   — settle as each fast-follow wedge lands; additive, so no rush.
2. Should `smith run` accept multiple positional prompts / a `--` separator for
   prompts that look like flags? (Lean: support `--` end-of-flags.)
3. Stream-json schema specifics — tracked by UX.md §23 Q4, not re-opened here.
4. Whether `smith config` edits project vs user scope by default (lean: `--user`
   flag, project by default to match D-CLI-6).
