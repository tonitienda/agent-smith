---
id: AS-123
title: TUI typewriter streaming — character-by-character reveal with trailing block cursor
status: done
github_issue: 388
depends_on: [AS-121, AS-021]
area: tui
priority: P0
source: docs/design/tui-visual-design.md §6, §7.2
---

# AS-123 — TUI typewriter streaming

## Problem

The TUI currently appends streamed assistant text to the transcript immediately as it
arrives. The spec requires a typewriter reveal (~30–50 ms/char) with a trailing `█`
block cursor on the last character while streaming is active. The "no animation over
finished content" rule (§6) means the cursor and drip must stop the instant the turn
ends.

## Goal

Make assistant replies appear letter-by-letter during streaming with a live block cursor,
giving the demo a visceral "agent thinking + writing" feel that distinguishes Agent Smith
from a static chat UI.

## What to build

### Typewriter buffer in the model

Add to `model`:

```go
typingBuf  []rune   // chars waiting to be revealed
typingIdx  int      // how many have been revealed so far
typingTick bool     // true when a Tick is scheduled
```

When a streaming token arrives (`loop.UITextDelta`, with `Text` carrying the fragment;
reasoning uses `loop.UIReasoningDelta` — see `internal/loop/event.go`), instead of
directly appending to the transcript block, push the runes into `typingBuf`.

### Tick-driven reveal

- Schedule a `tea.Tick(40*time.Millisecond, ...)` for each reveal step.
- On each tick: reveal the next rune from `typingBuf`, update the rendered transcript.
  If `typingBuf` is drained, don't schedule the next tick.
- If `typingBuf` fills faster than reveals (fast network, slow terminal), cap the drip at
  one rune per tick — never skip ahead. The user should feel a consistent typing cadence
  regardless of network speed.
- When the stream ends (`loop.UITurnComplete`), flush remaining
  `typingBuf` immediately so the full text appears without waiting for remaining ticks.
  This prevents the cursor hanging on finished content.

### Trailing block cursor

- While `len(typingBuf) > 0` or a reveal is pending, append `█` (in `ColorBrand`) to
  the last rendered line of the assistant turn in the transcript.
- Remove it once the buffer is flushed.

### Thinking / reasoning blocks

Reasoning text (tool call or thinking blocks) streams at the same rate but renders in
`StyleThinking` (`ColorMuted`) — no change to the colour rules, just the same drip.

### No animation over finished content

Once a turn is complete and `typingBuf` is empty:
- Stop all ticks for that turn.
- Remove the block cursor.
- The rendered text is static from that point on.

## Acceptance criteria

- `go test ./internal/tui/...` passes (add a test that verifies the buffer drains to
  empty on `TurnDone` and produces no trailing cursor glyph).
- Running `smith` and sending a message produces visible letter-by-letter text reveal
  with a `█` cursor at the end.
- Cursor disappears immediately when streaming ends.
- High-speed responses (large token bursts) still display at a comfortable 40 ms/char
  cadence rather than flashing all at once.
- `--no-splash` and headless mode are unaffected (typewriter is TUI-only).
