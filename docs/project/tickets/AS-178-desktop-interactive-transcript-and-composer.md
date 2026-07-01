---
id: AS-178
title: Desktop interactive transcript and composer
status: ready-to-implement
github_issue: null
depends_on: [AS-176, AS-177]
area: faces
priority: P1
source: docs/project/smith-desktop-wails-prd.md
---

# AS-178 · Desktop interactive transcript and composer

**Status: ready to implement**

## Description

Implement the first usable interactive session surface in the desktop app:
transcript, streaming assistant output, prompt composer, send/cancel, and basic
turn status.

This is the heart of the first desktop release. It should be simple, fast, and
clearly derived from Smith's existing event stream rather than inventing a
desktop-specific conversation model.

## Scope

- New-session entry point from the desktop shell.
- Transcript rendering for user and assistant turns.
- Streaming assistant output.
- Prompt composer with send and cancel.
- Turn-status presentation: idle, thinking, running, completed, failed.

## Acceptance criteria

- [ ] A user can start a session from the desktop app and send a prompt.
- [ ] Assistant output streams live in the transcript.
- [ ] The composer supports send and cancel.
- [ ] Turn state is visible without needing logs or developer tools.
- [ ] The transcript model is driven by `smith serve` events rather than a new
      desktop-only schema.

## Non-goals

- Session history/resume.
- Tool cards and permission prompts beyond placeholder wiring.

## Dependencies

- AS-176, AS-177.
