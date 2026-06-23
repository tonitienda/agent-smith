# Agent Smith TUI — design contract

This package renders Agent Smith's interactive terminal face (Bubble Tea / Lipgloss).
**Any change to how the TUI looks or animates MUST conform to the visual design reference.**

- Full spec + token table: `../../docs/design/tui-visual-design.md`
- Interactive reference (open in a browser): `../../docs/design/tui-reference.html`

Read the spec before touching styles. The rules below are the non-negotiable subset —
they apply to every TUI task.

## Non-negotiable invariants

1. **Palette is fixed — green + amber phosphor.** Define colors once as named lipgloss
   styles; never sprinkle raw hex at call sites. Core tokens:
   - brand / assistant green `#00ff66` · success `#00cc52` · command + bright `#7dffa8`
     · muted `#5f7a66` · neutral text `#9fb4a3`
   - user / warning amber `#ffb000` · goal + working `#caa24a`
   - default foreground `#c4e3cd` · screen background `#0a0e0b`
   Provide 256-color fallbacks via lipgloss profile detection — never assume truecolor.

2. **Role → color is semantic and stable.** user = amber, assistant = green,
   thinking/reasoning = muted green `#5f7a66`, tool name = `#9fb4a3` / args = `#5f7a66`
   / output = `#4f6a57`, success ✓ = `#00cc52`, running = `#ffb000`, slash-command =
   `#7dffa8`, cost = `#7dffa8`, goal = `#caa24a`. Do not recolor by aesthetics.

3. **The Matrix layer (AS-053) is CHROME-ONLY.** The personality — digital rain,
   scanlines, glitch, "Mr. Anderson" / idle phrases, operator glyph `▚▞` — may touch
   ONLY chrome and idle / empty / splash states. It must NEVER overlay transcripts,
   code, diffs, file contents, tool output, or data panels. **Substance always renders
   plain.** `/serious` mutes the entire layer instantly (plain names, no rain, no
   phrases).

4. **Intensity dial:** `subtle` (default) → `medium` → `bold`, additive. Ship `subtle`
   by default. Definitions in the spec.

5. **Liveliness = feedback, not decoration.** Every spinner, pulse, and typewriter
   effect must map to real state (a running tool, streaming tokens, an active subagent).
   No animation over static or finished content.

6. **Real character grid.** Use the documented glyph vocabulary
   (`▞▞ ✓ ├─ └─ │ ┃ █ ░ ▇ ● ◐ ○ ▚▞`) and box-drawing borders. Respect minimum legible
   sizing; never crush data below readable density.

When a needed state isn't covered, extend in the same vocabulary **and add it to the
spec** so the reference stays the source of truth.
