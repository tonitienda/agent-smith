---
id: AS-035
title: Lifecycle hooks (session, tool use, compact, prompt-submit)
status: done
github_issue: 35
depends_on: [AS-013, AS-018, AS-031]
area: capability
priority: P0
source: PRD.md §7.5
---

# AS-035 · Lifecycle hooks

**Status: ready to implement**

## Description

§7.5: user-configurable hooks at lifecycle events — **session start/stop, pre/post tool use, pre-compact, user-prompt-submit** — that can block, modify, or annotate.

- Hooks defined in config (AS-031): event + matcher (e.g., tool name pattern) + command to run.
- Contract: hook receives event payload as JSON on stdin; exit code + JSON stdout determine outcome — allow / block-with-reason (fed back to the model) / modify payload / annotate (append a note block to the log).
- Pre-tool-use hooks run after the permission check (AS-016) — permissions are the security boundary, hooks are automation.
- Timeouts and failure policy: a hanging or crashing hook never wedges the loop; failures are visible and configurable (fail-open vs fail-closed per hook).
- `pre-compact` fires before `/compact` (AS-038) and may veto or annotate.

## Acceptance criteria

- [ ] Each of the six PRD events fires at the right moment with a documented payload schema.
- [ ] A blocking pre-tool-use hook prevents execution and the model receives the reason.
- [ ] A modify hook altering tool input is applied and visible in the log (provenance: hook).
- [ ] Hook timeout kills the hook, applies the configured failure policy, and surfaces a warning.

## Dependencies

- AS-013 (tool runtime interception points), AS-018 (session/turn events), AS-031 (config)

## Implementation notes

- `internal/hook` is the runtime-agnostic core: it compiles the config `hooks`
  array into a `Set`, runs matching hooks per event (JSON payload on stdin),
  and folds their exit-code/stdout responses into one `Outcome` (allow / block /
  modify / annotate), resolving timeouts and failures through each hook's
  `failOpen` policy so a misbehaving hook never wedges the loop.
- Pre/post-tool-use are wired through `tool.WithPreToolHook`/`WithPostToolHook`
  seams in the runtime — pre runs *after* the permission gate; a modify rewrite
  is re-validated and recorded as a derived `tool_call` with hook provenance
  (PRD D3). Session start/stop and user-prompt-submit fire from the chat
  controller and the headless `smith run` path. Annotations land on the log as
  `hook_note` control events (`internal/eventlog`).
- **pre-compact** is defined and fires through the same `Set`, but the call site
  is `/compact`, which is not built yet (AS-038). AS-038 must call
  `hook.PreCompact` before compacting; tracked there rather than left as a TODO.
