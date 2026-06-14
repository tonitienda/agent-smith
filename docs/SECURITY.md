# Security posture

This document states Agent Smith's V1 security posture and its **explicitly
documented known limits**, per Decision Log **D9** (build the permission model
now) and **D0** (hard problems may be punted, but only as documented compromises
— never silently).

## Stated posture

> **Agent Smith runs with your privileges in your environment; you approve
> actions. It is not a sandbox.**

Agent Smith executes tools — reading and writing files, running shell commands —
as the user who launched it, in that user's environment, with that user's
permissions. There is no privilege boundary between the agent and the user. The
protection it offers is an **approval model**: you decide, per tool and per
action, what runs automatically and what must be confirmed first.

## The permission model (AS-016)

Every tool call passes through one gate before it runs. The tool runtime
(`internal/tool`, AS-013) invokes a permission hook for **every** execution,
after the call's arguments have been validated against the tool's schema and
before the tool's code runs. No execution path bypasses this gate (enforced by
test). The policy behind the hook lives in `internal/permission`.

### Modes (per tool)

A mode is resolved for each call: its per-tool override if configured, else the
session default, else **ask** (the safe default).

| Mode | Behavior |
|---|---|
| `auto` | Every call runs; no prompt. |
| `allowlist` | A call matching an allow-rule runs automatically; everything else prompts. |
| `ask` | Every call prompts. Allow-rules are **not** auto-applied. |

Modes are switchable per session (the default) and per tool (an override map),
so you can, for example, run `read` on `auto` while keeping `shell` on `ask`.

When a call needs a prompt but no interactive approver is wired (a headless or
non-interactive run), the call is **denied** with a model-readable reason rather
than blocking forever. Non-interactive runs should therefore use `auto`, or
`allowlist` with an allow-list that covers the work.

### Allow-rules

An allow-rule is a tool name plus an optional pattern matched against the call's
*subject* — the shell command line for `shell`, the file path for the file
tools. Rules come from layered config (user-level, then project-level, merged
with project winning) and grow at runtime when you choose **"always allow this"**
at a prompt (which appends the rule to the project config).

- **Shell** patterns are **prefix** matches: `git status*` allows `git status`
  and any arguments after it. A pattern without a trailing `*` must match the
  command exactly.
- **File-path** patterns are **globs**: `*` and `?` match within a path segment,
  `**` spans segments, `/` is the separator. `docs/**` allows writes anywhere
  under `docs/`; `*.go` allows a top-level Go file.
- An empty pattern (or `*`) matches any call of that tool. A tool name of `*`
  matches any tool.

Prefix matching is literal and not word-boundary aware: `git status*` also
matches `git statusfoo`. Write allow-rules with this in mind.

### Denials

A denied call is returned to the model as a tool-result error carrying the
reason, so the model can adjust its plan rather than retry the same call blindly.
Denial is feedback, not a crash.

### Configuration

Permission config is JSON, loaded from two layers and merged (project over
user):

- **User:** `$XDG_CONFIG_HOME/agent-smith/permissions.json` (falling back to
  `~/.config/agent-smith/permissions.json`).
- **Project:** `<project-root>/.smith/permissions.json` — also where
  "always allow this" rules are appended.

```json
{
  "default_mode": "allowlist",
  "tools": { "shell": "ask", "read": "auto" },
  "allow": [
    { "tool": "read" },
    { "tool": "shell", "pattern": "git status*" },
    { "tool": "write", "pattern": "docs/**" }
  ]
}
```

The schema is additive-only (Decision Log D2): new capabilities arrive as new
optional fields, so an older binary ignores fields it does not understand and a
newer config still loads in an older one.

## Known limits (V1)

These are **deliberate, documented punts** (D9), not oversights. The posture
above assumes them.

- **No OS-level sandbox.** Tools run with the user's full privileges. A shell
  command approved (or auto-allowed) can do anything the user can. Approval is
  the only boundary; there is no syscall/filesystem/network confinement.
- **No prompt-injection defense.** Content the model reads — file contents, tool
  output, fetched data — may contain instructions that attempt to steer the
  agent into taking actions you did not intend. V1 has no mechanism to detect or
  neutralize this. The permission model is the mitigation: keep destructive
  tools on `ask` or a tight `allowlist` so an injected instruction still has to
  pass your approval.
- **No plugin code-sandboxing.** V1 sub-agents are first-party. Third-party
  plugins, when they arrive, are **declarative-only** (manifest + prompt, no
  arbitrary code) precisely because there is no sandbox to contain plugin code.
- **Lexical path containment only.** The file tools confine paths to the project
  root with a lexical check; a symlink pointing outside the root is not
  resolved-and-rechecked in V1 (see `internal/tool/builtin`). Do not run the
  agent in a tree containing symlinks to sensitive locations you would not want
  written.
- **Secrets / key storage.** OS-keychain-backed API key storage is tracked
  separately (AS-017) and is not covered by this document.

## Guidance

- Keep `default_mode` at `ask` or `allowlist` for interactive use; reserve
  `auto` for throwaway or already-isolated environments (a container, a VM, a
  scratch checkout).
- Prefer narrow allow-rules (`git status*`) over broad ones (`git*`), and
  whole-tool allows only for genuinely safe, read-only tools.
- Treat anything the model reads as untrusted input. The model can be tricked;
  your approval cannot be, so keep the dangerous tools gated.

## Reporting

This is a learning-oriented but shippable project. If you find a security issue,
open an issue describing it, or contact the maintainer at the address in the
repository metadata.
