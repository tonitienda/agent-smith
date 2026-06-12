---
id: AS-024
title: TUI tool-call transparency, diff review, and permission prompts
status: ready-to-implement
github_issue: null
depends_on: [AS-021, AS-016]
area: tui
priority: P0
source: PRD.md §7.8, D9
---

# AS-024 · TUI tool transparency, diff review, permission prompts

**Status: ready to implement**

## Description

The trust surface of the TUI (§7.8): the user must always see what the agent is doing and approve what's risky.

- Tool calls rendered as collapsible entries: name + summarized args while running, result preview when done; expand for full output.
- File edits/writes presented as unified diffs (syntax-aware coloring) before/after application.
- Permission `ask` prompts (from AS-016) rendered as a modal: allow once / always allow (writes the allowlist rule) / deny — with the exact command/path shown, never a paraphrase.
- Denied actions visibly marked in the transcript.

## Acceptance criteria

- [ ] Every tool call in a turn is visible and expandable in the transcript.
- [ ] An `edit` shows a correct diff before the permission prompt resolves.
- [ ] "Always allow" persists the rule and subsequent matching calls skip the prompt.
- [ ] The exact shell command string is shown verbatim in the prompt.

## Dependencies

- AS-021 (TUI), AS-016 (permission decisions to render)
