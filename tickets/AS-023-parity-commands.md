---
id: AS-023
title: "Parity commands: /clear, /model, /resume"
status: ready-to-implement
github_issue: null
depends_on: [AS-007, AS-008, AS-022]
area: commands
priority: P0
source: PRD.md §7.16, Appendix A, D6
---

# AS-023 · Parity commands: /clear, /model, /resume

**Status: ready to implement**

## Description

The V1 subset of parity power commands (D6: "a subset of 7.16"). `/cost` ships with AS-020; `/compact`, `/rewind`, `/init`, `/goal`, `/budget`, `/route` are fast-follow.

- **/clear** — end the current session and start a fresh one (new log). The old session remains on disk and resumable; nothing is deleted (append-only ethos).
- **/model** — list configured providers/models; switch mid-session. The switch is recorded as an event in the log so cost attribution and the transcript stay accurate. Works across providers (Anthropic ↔ OpenAI) thanks to the normalized block schema.
- **/resume** — list recent sessions for this project (title, age, cost, size) and load one; also `smith --resume <id>` from the CLI.

## Acceptance criteria

- [ ] `/clear` starts a clean context; the previous session appears in `/resume`.
- [ ] `/model` switches provider mid-session and the next turn uses the new model; the event log records the switch.
- [ ] A session started on Anthropic resumes and continues on OpenAI without transcript corruption (the bilingual-schema payoff, D4 — test this explicitly).
- [ ] `/resume` restores a session whose projection is identical to its last live state.

## Dependencies

- AS-007 (persistence), AS-008 (per-request model selection), AS-022 (command framework)
