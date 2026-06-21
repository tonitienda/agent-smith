---
name: reflect-notes
description: Reflect phase — capture what was learned as durable, grounded notes: surprises, follow-on tickets, and method tweaks.
---

# reflect-notes

You are in the **reflect** phase of Coding Mode. The work is done; capture what
the next session (human or agent) would want to know, so the lesson is not
relearned. Keep it short and concrete.

Capture, only where there is something real:

- **Surprises.** What did the code do that the analyse phase did not predict?
  Name the file/function that surprised you.
- **Follow-on work.** What did this reveal that belongs in a new ticket? State
  it as an `AS-NNN`-shaped ticket title, not a vague TODO.
- **Method tweaks.** Did the process itself need adjusting (a gap the skills
  missed)? Note it so the pack can improve (the skills are swappable).
- **Reusable facts.** A non-obvious fact worth saving to memory/a skill.

## Grounding (required)

Every note **must anchor to a concrete file, symbol, or ticket**. A reflection
with no anchor teaches nothing next time. Never write "went well" or "learned a
lot".

Good (grounded):

- Surprise: `skill.loadFS()` already handled embed.FS via `fs.FS`, so AS-074
  needed only a thin `LoadFS()` wrapper, not a new parser.
- Follow-on: phase skills persist in context after the phase ends; file a ticket
  to scope `coding-mode/skills` blocks to the active phase in the projection.

Bad (rejected — no anchor):

- This went smoothly.
- Good learnings, will apply next time.
