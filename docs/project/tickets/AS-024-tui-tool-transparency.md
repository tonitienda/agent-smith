---
id: AS-024
title: TUI tool-call transparency, diff review, and permission prompts
status: done
github_issue: 24
depends_on: [AS-021, AS-016, AS-067]
area: tui
priority: P0
source: PRD.md §7.8, D9
---

# AS-024 · TUI tool transparency, diff review, permission prompts

**Status: done**

## Description

The trust surface of the TUI (§7.8): the user must always see what the agent is doing and approve what's risky.

- Tool calls rendered as collapsible entries: name + summarized args while running, result preview when done; expand for full output.
- File edits/writes presented as unified diffs (syntax-aware coloring) before/after application.
- Permission `ask` prompts (from AS-016): normal actions render as an **inline transcript card** (allow once / always allow / deny); destructive or broad-scope actions escalate to a **blocking modal** (focus-trapped, severe styling) reusing the AS-067 modal overlay. Either way the exact command/path is shown verbatim, never a paraphrase. (TUI-UX.md D-TUI-8.)
- Denied actions visibly marked in the transcript.

## Acceptance criteria

- [x] Every tool call in a turn is visible and expandable in the transcript.
- [x] An `edit` shows a correct diff before the permission prompt resolves.
- [x] "Always allow" persists the rule and subsequent matching calls skip the prompt.
- [x] The exact shell command string is shown verbatim in the prompt.

## How it was built

- Tool cards (`internal/tui/transcript.go`): each `segTool` carries a one-line
  argument summary (shown while running) and the recorded result text (previewed
  to `toolPreviewLines` when done). The leader chord `Ctrl+G t` toggles full
  output (`model.expandTools`). Denied/failed calls already render with the `✗`
  marker and now show the denial reason in the result preview.
- Permission prompts (`internal/tui/permission.go`): the loop's permission
  `Asker` is bridged into the Update loop via `App.Ask` (a buffered reply channel
  through `tea.Program.Send`). A normal action renders as an inline transcript
  card the user can scroll context behind; a **destructive/broad-scope** action
  (the `shell` tool) escalates to the focus-trapped blocking modal (D-TUI-8).
  Parallel tool calls (AS-019) queue and are decided one at a time. The exact
  command/path is shown verbatim; an `edit` shows a `-`/`+` diff of its
  replacement (AC2).
- Wiring (`cmd/smith/permission.go`, `controller.go`, `chat.go`): a single
  `permission.Policy` is built per session from the layered config and reused
  across engine rebuilds (`/clear`, `/model`, `/resume`), so a remembered "always
  allow" sticks. The TUI stays face-agnostic — the `permission.Asker` adapter and
  the edit-diff rendering live in the command, behind the `tui.PermissionPrompt`
  seam, so `internal/tui` imports neither `internal/permission` nor
  `internal/tool`.
- Default posture: with no `.smith/permissions.json`, the mode is `ask`, so every
  tool call prompts (PRD D9 conservative default). A project relaxes it with an
  allowlist/auto config or per-tool overrides (AS-016).

## Dependencies

- AS-021 (TUI), AS-016 (permission decisions to render), AS-067 (panel host + modal overlay infra reused for destructive prompts)
