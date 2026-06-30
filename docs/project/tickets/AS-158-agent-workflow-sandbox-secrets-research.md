---
id: AS-158
title: Competitive agent workflow, sandbox, and secrets research spike
status: done
area: research
priority: P2
depends_on: [AS-159]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-158 · Competitive agent workflow, sandbox, and secrets research spike

## Description

Research how current agent workflow systems handle always-on jobs, repository automation, sandboxes, secrets, and multi-provider execution so Smith's dogfood orchestrator does not reinvent avoidable mistakes.

## Acceptance criteria

- [x] Research notes cover Anthropic/Claude Code, OpenAI/Codex, Cursor, Coder, Ona, and any adjacent tools that expose relevant workflow/sandbox/secrets patterns. (Also covers Copilot coding agent, Devin, Jules.)
- [x] Compare scheduling, GitHub triggers, PR creation/update, review/merge policy, sandbox isolation, credentials, environment variables, secret redaction, artifact retention, and audit logs.
- [x] Identify which patterns Smith should copy, avoid, or intentionally differ from.
- [x] Feed concrete recommendations into AS-148, AS-153, AS-154, AS-156, and AS-157.
- [x] Document unresolved unknowns and links to primary docs where available.

## Outcome

Research notes: [docs/research/orchestrator-competitive-research.md](../../research/orchestrator-competitive-research.md)
— comparison matrix across the 10 axes, copy/avoid/differ findings, per-ticket
recommendations (AS-148/153/154/156/157), and an unresolved-unknowns list. Each
fed ticket carries a "Research input (AS-158)" pointer to the relevant section.

## Dependencies

[AS-159]
