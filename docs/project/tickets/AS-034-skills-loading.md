---
id: AS-034
title: Portable skills — discovery, matching, and loading
status: done
github_issue: 34
depends_on: [AS-018, AS-031]
area: capability
priority: P0
source: PRD.md §7.7, §4
---

# AS-034 · Portable skills

**Status: done**

## Implementation notes

- `internal/skill` discovers `SKILL.md` skill directories from the user
  (`<UserConfigDir>/smith/skills/<name>/`) and project (`.agent-smith/skills/<name>/`)
  locations, project winning on a name collision. Frontmatter `name`/`description`
  are read; any extra keys (e.g. `expected_outcome`, `completion`) are preserved in
  `Skill.Meta` for AS-047. Claude-Code-style skills load unmodified.
- The model invokes a skill through a single `skill` tool (wired in
  `cmd/smith/skills.go`): its description lists the available skills, and calling
  it with `{"name": ...}` returns that skill's instruction body. The body enters
  context as the tool_result, attributed to the skill (`Attribution.Skill`), so
  `/context` shows it under a dedicated **skill** group, origin `skill: <name>`,
  with token cost. `tool.Output` gained an `Attribution` field the runtime merges
  onto the result (the tool's own name stays authoritative); MCP (AS-036) reuses it.
- Skill availability is recorded on the log as `eventlog.KindSkillLoad` control
  events (non-rendered, attributed by skill name), seeded on fresh sessions
  alongside memory files — the stable hook AS-047's analyzers attach to. The
  activation span is the attributed `skill` tool_call/tool_result pair.
- Headless `smith run` denies tool calls (D-CLI-8), so the skill tool is not
  offered there.

## Description

§7.7: load portable skills (instructions + optional tools); the model invokes them on match. Skills are also the substrate living-skills (AS-047/048/049) builds on, so loading must record clean span boundaries in the event log.

- Discovery: skill directories (`SKILL.md` with `name` + `description` frontmatter, optional supporting files) from project and user locations.
- Compatibility goal: Claude-Code-style skills load unmodified where features overlap (§4 "portable").
- Matching: skill names/descriptions exposed to the model; on invocation the skill body enters the context as attributed blocks (visible in `/context` as a skill segment).
- Skill-load and skill-active span recorded as events in the log — the hook point AS-047's contracts and the analyzers attach to.
- Frontmatter fields beyond name/description (e.g., `expected_outcome`, `completion`) are preserved/passed through but interpreted by AS-047, not here.

## Acceptance criteria

- [ ] A skill triggers on a matching request and its instructions demonstrably shape the response.
- [ ] Skill content is attributed in `/context` with token cost.
- [ ] Skill load/activation events appear in the log with stable IDs.
- [ ] A representative Claude Code skill loads and runs unmodified.

## Dependencies

- AS-018 (loop), AS-031 (config/paths)
