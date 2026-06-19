---
id: AS-085
title: Auto-compact on approaching the window limit (config-flagged, default off)
status: done
github_issue: 144
depends_on: [AS-038, AS-025, AS-031]
area: commands
priority: P2
source: PRD.md §7.16
---

# AS-085 · Auto-compact on approaching the window limit

**Status: ready to implement** — spun out of AS-038, whose v1 scope shipped the manual `/compact` (preview/apply/undo) but deferred the optional automatic trigger.

## Description

AS-038 delivered `/compact` as a user-invoked command. The remaining bullet from that ticket is the *automatic* trigger: when the projected context approaches the model's window limit, compact the older span on the agent's behalf so the turn doesn't fail with a context-window-exceeded stop.

- Off by default — the product thesis prefers `/clean`/`/tidy`; auto-compact is the blunt instrument of last resort (PRD §7.16). Gate it behind a layered-config flag (AS-031), e.g. `compact.auto = true` plus a threshold (`compact.auto_threshold`, fraction of the window).
- Reuse the AS-038 engine unchanged: detect the threshold from the context meter accounting (AS-025), then run the same `compact.Preview` → cheap-tier summarize → `compact.Build` path the manual command uses, so the result is the same reversible compaction block (still `/compact --undo`-able).
- Surface it: the auto-compaction must be visible (a transcript/status note), never silent (D0). Record it on the log attributed distinctly from the manual `/compact` so `/insights` (AS-045) can tell them apart.

## Acceptance criteria

- [x] With the flag off (default), behaviour is identical to today — nothing auto-compacts.
- [x] With the flag on, crossing the threshold triggers one compaction of the older span before the next turn, and the turn then proceeds.
- [x] The auto-compaction is reversible (`/compact --undo`) and visibly surfaced, never silent.
- [x] Its summarization cost is itemized in `/cost` like the manual path.

## Implementation notes

- Config: `compact.auto` (bool, default off) + `compact.auto_threshold` (window fraction, default 0.85 when unset or outside (0,1)). Read once at startup in `cmd/smith/chat.go`; both `budget` and `compact` were added to `knownConfigSections` (`cmd/smith/cli.go`) so the sections don't warn as unknown.
- The pre-turn check lives in `chatSession.maybeAutoCompact` (`cmd/smith/controller.go`), invoked from `Run` after the user-prompt-submit hook and before the engine turn. It builds the same `composition` the meter/`/context` use to compare projected tokens against the model window, then runs the unchanged AS-038 `compact.Preview` → `summarize` (cheap tier) → `compact.Build` path.
- Reversibility: the compaction block keeps `compact.Producer` (`/compact`), so `/compact --undo` reverses an auto-compaction identically. Only the summarization **usage** event is attributed distinctly, via the new `compact.AutoUsageProducer` (`/compact (auto)`), so `/cost` itemizes it and `/insights` (AS-045) can separate auto from user-invoked spend.
- Surfacing (D0): a new additive `loop.UIAutoCompact` UI event carries the notice; the TUI renders it as a transcript notice line. Best-effort — a summarizer error surfaces a notice and the turn proceeds with the full context rather than failing.

## Dependencies

- AS-038 (`/compact` engine + command), AS-025 (context meter accounting for the threshold), AS-031 (config flag)
