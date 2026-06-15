---
id: AS-024
title: TUI tool-call transparency, diff review, and permission prompts
status: ready-to-implement
github_issue: 24
depends_on: [AS-021, AS-016, AS-067]
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
- Permission `ask` prompts (from AS-016): normal actions render as an **inline transcript card** (allow once / always allow / deny); destructive or broad-scope actions escalate to a **blocking modal** (focus-trapped, severe styling) reusing the AS-067 modal overlay. Either way the exact command/path is shown verbatim, never a paraphrase. (TUI-UX.md D-TUI-8.)
- Denied actions visibly marked in the transcript.

## Acceptance criteria

- [ ] Every tool call in a turn is visible and expandable in the transcript.
- [ ] An `edit` shows a correct diff before the permission prompt resolves.
- [ ] "Always allow" persists the rule and subsequent matching calls skip the prompt.
- [ ] The exact shell command string is shown verbatim in the prompt.

## Dependencies

- AS-021 (TUI), AS-016 (permission decisions to render), AS-067 (panel host + modal overlay infra reused for destructive prompts)
