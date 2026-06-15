# Agent Smith UX

> Status: **draft for PR review**  
> Scope: product/interface design, TUI-first UX, face-neutral architecture  
> Date: 2026-06-15  
> Owner: Toni

Agent Smith is a provider-neutral coding-agent harness built around an open,
append-only session substrate. The flagship interactive face is the terminal UI,
but the product should not be designed as a terminal-only application. The same
core events, projections, commands, permissions, and view models should support
headless CLI, ACP/editor integrations, and possible Desktop/Web faces later.

This document defines the UX direction with an interactive TUI first, while
preserving the architectural seams needed for programmable use and future richer
visual interfaces.

---

## 1. Product UX thesis

Agent Smith is a professional terminal cockpit for supervising agentic coding
work: one orchestrator, many possible workers, all observable through an open
session substrate.

The UX is not "a nicer chat box." It is a harness interface that helps the user
understand and control what agentic work is doing:

- What is the agent doing?
- Which subagent or worker is doing it?
- What context is being used?
- What context is becoming stale, duplicated, or expensive?
- What risky action needs approval?
- What changed in the repository?
- What can be resumed, cleaned, rewound, or reused?

The terminal UI is the first and most important interactive face, but it should
feel like a cockpit over the core system, not the place where product semantics
live.

**Core sentence:** Agent Smith should make agent work observable, controllable,
portable, and reusable.

---

## 2. Experience principles

### 2.1 Alive, not noisy

The TUI should feel alive while work is happening. It should stream assistant
output, update tool cards, show running subagents, refresh context/cost meters,
and make state transitions visible.

Liveliness is feedback, not decoration. Motion, spinners, and personality belong
in chrome and transient state indicators. They must never interfere with code,
diffs, commands, tool arguments, JSON output, permission prompts, or machine
readable output.

### 2.2 Professional terminal cockpit

The default feeling should be a serious terminal tool: closer to lazygit, k9s,
Claude Code, and an observability dashboard than to a cyberpunk toy.

Agent Smith may have a subtle Matrix-inspired identity, but it should remain
secondary to trust and clarity.

### 2.3 Visually useful, not visually decorative

Visual richness should come from useful data views:

- context composition
- cleanup suggestions
- token/cost breakdowns
- duplicate and stale context detection
- tool timelines
- subagent state
- diff review
- session insights

The product should win because it makes hidden agent state visible.

### 2.4 Supervisory by default

The primary posture is supervision. The user gives Agent Smith a task, watches
it work, interrupts when needed, approves risky actions, inspects context, and
reviews results.

Chat and research remain first-class, but the differentiating experience is
supervising agentic work, not simply exchanging messages.

### 2.5 Programmable at the seams

Anything useful in the TUI should have, where reasonable, a non-interactive
equivalent:

- slash commands map to command handlers
- panels map to structured view models
- transcript items map to session events
- command outputs can render as plain text, JSON, or stream JSON
- headless sessions are normal sessions that can be resumed in the TUI

Headless mode must have no ANSI decoration, spinners, prompts, mascot text, or
personality by default.

### 2.6 Core owns truth; faces render it

The append-only event log is the source of truth. Model-facing context is a
projection over that log. The TUI renders projections and submits user decisions;
it does not own provider behavior, tool behavior, permission policy, cost
tracking, or context mutation semantics.

### 2.7 Reversible over destructive

Context-control operations should append events or derive projections. They
should not silently mutate or delete history.

Commands such as `/clean`, `/tidy`, `/compact`, and `/rewind` should explain what
will change, show token impact where possible, and provide an undo or restoration
path.

---

## 3. Face strategy

### 3.1 TUI first

The TUI is the flagship interactive face. It should be enjoyable for daily use,
fast to operate from the keyboard, and rich enough to show the product wedge:
context observability and control.

### 3.2 Headless CLI second

The next face after the TUI should be the programmable CLI. This keeps the
architecture honest by forcing the TUI and headless mode to share events,
commands, permissions, and output schemas.

> **The CLI contract is settled in [docs/project/CLI-UX.md](project/CLI-UX.md)**
> (subcommand-first/noun-grouped, bare `smith` launches the TUI, TTY-aware output,
> stdout=data/stderr=diagnostics, exit codes, config precedence). It supersedes
> the flag-driven `smith -p "…"` direction sketched below — `smith run` takes the
> prompt as a positional arg / stdin / file, and there is no `-p` flag.

Example direction (per CLI-UX.md):

```sh
smith run "fix the failing test"                 # plain on a TTY, bare when piped
echo "summarize context" | smith run --output json
smith run "run the task" --output stream-json --budget 0.25
```

Headless CLI should optimize for pipes, CI, scripts, dashboards, and scheduled
automation.

### 3.3 ACP/editor integrations later

ACP/editor integrations should reuse the same session substrate, command
registry, permission API, context projections, and render models. Editors may
provide richer file navigation, but they should not become a separate source of
product semantics.

### 3.4 Desktop/Web later, but not blocked

Desktop and Web are not V1 implementation goals, but the core must not work
against them.

Future Desktop/Web may become valuable for:

- richer context dashboards
- better diff and review workflows
- onboarding users who are less terminal-native
- collaboration and team visibility
- orchestration graphs and timelines
- cross-session analytics

The TUI should therefore not expose terminal-only data structures as core
contracts. It should render face-neutral view models that Desktop/Web can later
reuse.

---

## 4. Default TUI direction

The TUI should feel like:

> lazygit/k9s + Claude Code + observability dashboard, with subtle Agent Smith
> identity.

The product should be usable and clear in a normal terminal around 100 columns.
It should enhance gracefully on wider terminals.

### 4.1 V1 layout

V1 should start with a simple, stable shell:

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ transcript / active work log                                                 │
│                                                                              │
│ user messages, assistant messages, tool cards, diffs, permission decisions,   │
│ command outputs, session events, subagent events                              │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ project · session · provider/model · agents · context · cost · state          │
├──────────────────────────────────────────────────────────────────────────────┤
│ prompt / slash command / palette                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

This layout should work in common terminals without assuming large screens.

### 4.2 Adaptive direction

The long-term product direction should support adaptive layouts:

- normal/narrow terminal: transcript shell + full-screen panels
- wide terminal: transcript shell + optional side panel
- Desktop/Web: richer dashboards over the same view models

V1 can implement full-screen panels first. The architecture should still model
panels as reusable views, not hard-coded terminal screens.

### 4.3 Work mode and inspect mode

The TUI has two modes of attention.

**Work mode** is the normal transcript shell:

- prompt
- assistant streaming
- running tools
- subagent activity
- status line
- approval prompts

**Inspect mode** uses full-screen panels:

- `/context`
- `/agents`
- `/diff`
- `/resume`
- future `/insights`
- future `/skills`
- future `/route`

The user should be able to switch between work and inspection without losing the
sense of what is currently running.

---

## 5. Visual identity and tone

### 5.1 Tone

Primary tone:

- precise
- calm
- competent
- trustworthy

Secondary tone:

- alive
- slightly playful
- distinctive

Never:

- evasive
- cute during risky actions
- noisy
- theatrical
- vague about what the agent is doing

### 5.2 Agent Smith / Matrix identity

A subtle Matrix-inspired identity is allowed in chrome only.

Allowed:

- startup ASCII art
- small abstract mascot/operator icon
- status avatar
- empty states
- optional idle/status phrases
- theme names
- non-critical loading chrome

Forbidden:

- generated code
- diffs
- shell commands
- commit messages
- file writes
- tool arguments
- JSON output
- headless output
- permission risk descriptions
- error payloads where precision matters

Example startup direction:

```text
   A G E N T   S M I T H
   provider-neutral coding harness

   project: agent-smith
   model:   claude-sonnet
   mode:    interactive
```

A small quote or playful phrase can be considered later, but startup should not
slow down daily use.

### 5.3 Serious mode

A future serious mode should disable all personality and produce the plainest
possible interactive experience.

Possible command/config:

```text
/serious
```

```yaml
serious_mode: true
```

---

## 6. Transcript model

The transcript is not merely chat history. It is a readable projection of the
append-only event log.

Live execution and historical replay should converge. A resumed session should
render with the same transcript item renderers as a live session.

### 6.1 Transcript item types

The transcript should support at least:

- user message
- orchestrator message
- subagent message
- assistant streaming text
- finalized markdown
- reasoning summary if exposed and allowed
- tool call
- tool result
- file read
- file edit proposal
- diff review result
- permission request
- permission decision
- command output
- model switch
- session clear
- resume
- clean/tidy/compact/rewind event
- error
- cancellation
- budget or policy stop

### 6.2 Attribution

Every transcript item should be attributable.

At minimum, events should be able to identify:

- actor ID
- actor display name
- actor role
- parent task or parent agent where applicable
- whether the actor is the orchestrator, a subagent, a tool, or the user

Example:

```text
Smith / Orchestrator
  I’ll split this into schema review, TUI model, and tests.

◐ Researcher
  reading docs/project/PRD.md

◐ Implementer
  editing internal/tui/context_panel.go

✓ Tester
  go test ./... · passed · 18s

Smith / Orchestrator
  The implementation is ready. Diff review required.
```

Without attribution, multi-agent sessions become unreadable.

### 6.3 Collapsing and expansion

The transcript should remain readable during long-running tasks.

Default behavior:

- compact cards for tool calls
- short previews for tool results
- expandable long output
- collapsed subagent internal chatter unless important
- final orchestrator summaries for completed subagent work

The user should be able to expand details when needed, but should not be forced
to read every internal step.

---

## 7. Orchestration and subagents

Agent Smith should be designed as an orchestrator of agents, not only as a single
agent.

The interactive TUI may initially expose one primary assistant, but the
underlying UX model must support multiple active workers:

- orchestrator
- researcher
- implementer
- reviewer
- tester
- context cleaner
- future custom subagents
- future system subagents

The user should always be able to answer four questions:

1. Who is working?
2. What are they doing?
3. What context are they using or producing?
4. What decision do they need from me?

### 7.1 Orchestrator role

The orchestrator is the user's main counterpart. It should coordinate work,
summarize subagent activity, ask for approval, and avoid making the user mentally
reconstruct a parallel execution tree from raw logs.

The orchestrator should be responsible for:

- explaining the plan
- delegating to subagents
- summarizing subagent findings
- deciding what should enter shared context
- requesting user decisions
- presenting final results

### 7.2 Subagent visibility

Subagent activity should be visible but not overwhelming.

Default transcript behavior:

- show subagent start
- show current high-level action
- show tool calls that matter
- show failures and blocked states
- show final subagent summary
- collapse repetitive internal chatter

Example:

```text
◐ Researcher · docs/code
  Inspecting PRD and TUI tickets...

✓ Researcher · docs/code
  Found 4 relevant decisions:
  - TUI must consume face-agnostic events
  - command registry is face-neutral
  - context meter uses a seam
  - permission/diff review are near-term trust work
```

### 7.3 `/agents` panel

A future `/agents` panel should show active, queued, completed, and failed
workers.

Example:

```text
Agents · current task: "Design TUI architecture"

  name          role           state       cost    ctx     last action
  Orchestrator  coordinator    waiting     $0.08   12k     needs approval
  Researcher    docs/code      done        $0.03   8k      PRD summarized
  Implementer   code edits     running     $0.07   18k     editing TUI model
  Reviewer      critique       queued      $0.00   0k      waiting for diff
```

This panel is not required for the earliest TUI slice, but the event and render
models should include actor identity from the beginning.

### 7.4 Agent graph and timeline

Future richer faces may render orchestration as:

- task tree
- agent graph
- parallel timeline
- review queue
- dependency graph
- per-agent cost/context dashboard

The TUI should not need to implement these early, but the core should preserve
relationships such as parent task, child subagent, delegated goal, result, and
promotion to shared context.

---

## 8. Status line

The status line should be calm, dense, and useful.

Baseline fields:

- project or directory
- session title or short ID
- provider/model
- active agent/subagent summary
- context occupancy
- session cost or unknown marker
- current state

Example single-agent state:

```text
agent-smith · main · claude-sonnet · ctx 42k/200k · $0.18 · running tool
```

Example orchestration state:

```text
agent-smith · 3 agents · 2 running · 1 waiting approval · ctx 42k/200k · $0.21
```

Possible states:

- idle
- thinking
- planning
- running tool
- running subagents
- waiting approval
- reviewing diff
- cancelled
- offline
- error
- budget stopped
- permission stopped

The status line is a good place for subtle identity, but it must stay readable.

---

## 9. Input layer and command palette

### 9.1 Prompt editing

The input layer should support:

- multi-line editing
- history navigation
- paste handling
- slash-command palette
- quoted command arguments
- cancellation
- clear visual distinction between prompt mode and command mode

### 9.2 Enter behavior

Default decision:

```text
Enter       submit
Alt+Enter   insert newline
Esc         close panel/modal -> cancel in-flight turn -> clear input state
```

This optimizes for coding-agent familiarity. Long composition can be supported
later by a full-screen compose panel.

### 9.3 Slash commands

Slash commands should be discovered through a face-neutral command registry, not
hard-coded in the TUI.

A command should declare:

- name
- aliases
- summary
- argument schema
- scriptability metadata
- output schema where applicable
- permission requirements
- side effects
- whether it is interactive-only, headless-capable, or both

The TUI command palette renders this metadata. Headless CLI and future faces
should use the same registry.

### 9.4 Keymap direction

Chat basics should feel familiar to users of coding agents.

Panels should feel closer to lazygit/k9s:

- keyboard-first
- fast navigation
- filtering
- sorting
- selection
- preview
- action shortcuts
- documented help

Mouse support is not required in V1, but the architecture should not prevent it.

---

## 10. Tool transparency

Tool calls should be first-class cards, not raw logs.

### 10.1 Collapsed card

```text
▸ read_file  internal/tui/model.go                     1.2k tokens · 38 ms
```

### 10.2 Running card

```text
◐ shell  go test ./...                                  running · 00:12
  $ go test ./...
```

### 10.3 Completed card

```text
✓ shell  go test ./...                                  148 lines · 00:34
```

### 10.4 Failed or denied card

```text
✗ shell  rm -rf build                                   denied by user
```

Rules:

- show exact command strings and paths before approval
- show requesting actor when applicable
- keep previews deterministic and short
- make full output expandable
- never flood transcript by default
- derive tool state from core events, not TUI-specific logic

---

## 11. Permissions

Permission prompts are trust-critical. They may follow the active theme visually,
but they must remain exact, serious, and trust-preserving.

A permission prompt should show:

- requesting actor
- action type
- exact command/path/scope
- stated reason
- risk class if available
- available choices
- whether a choice persists an allowlist rule

Example:

```text
Permission required: shell

Requested by:
  Tester

Command:
  go test ./...

Reason:
  Verify the edit batch before review.

[Allow once] [Always allow matching] [Deny]
```

For destructive or broad actions, the prompt should feel visually severe even
when a theme is active.

Rules:

- never paraphrase risky commands instead of showing them
- distinguish allow-once from persistent allowlist changes
- record the decision in the transcript
- include actor attribution
- headless mode never opens an interactive prompt

Headless mode should deny, stop, or follow explicit auto/allowlist policy.

---

## 12. Diff review

File writes and edits should present unified diffs before application when
permission policy requires review.

V1 decision: approve or reject the full edit batch.

Per-file and per-hunk approval are revisit items.

Example:

```text
Edit proposal · 2 files · +42 -18

  internal/config/load.go       +12 -4
  internal/config/load_test.go  +30 -14

[View diff] [Apply] [Reject] [Ask Smith to revise]
```

Diff view should support:

- file-level navigation
- unified diff rendering
- syntax-aware color where available
- clear additions/removals
- batch approval/rejection
- actor attribution
- reason for edit where available

Rules:

- the diff itself must not contain decorative personality
- file paths and change counts must be clear
- approval result is recorded as an event
- the edit proposal should be replayable from the event log

---

## 13. Context observability

Context observability is the flagship wedge of Agent Smith.

The `/context` panel should answer quickly:

> What is eating my context, why is it there, and what can I safely clean?

### 13.1 Default `/context` view

The default view should be a dashboard summary plus sortable table.

Example:

```text
Context · 42.1k / 200k tokens · $0.18 session · 6.2k likely reclaimable

Top consumers
  1. tool result: go test ./... failure output          8.4k  20%
  2. file: docs/project/PRD.md                          5.9k  14%
  3. duplicate reads: internal/tui/model.go ×3          3.1k   7%

Segments
  type          origin                     owner        age    tokens  status
  tool-result   go test ./...              Tester       8m     8.4k    stale?
  file-read     docs/project/PRD.md        Researcher   12m    5.9k    shared live
  file-read     internal/tui/model.go      Researcher   4m     1.2k    duplicate
```

### 13.2 Required affordances

The context panel should support:

- grouping by type
- grouping by file/origin
- grouping by owner/agent
- sorting by size
- sorting by age
- sorting by status
- duplicate detection
- stale candidate detection
- shared vs private context distinction
- multi-select for cleanup
- preview before mutation
- no model call required to open the basic panel

### 13.3 User-friendly language

Default labels should be user-friendly:

- "working set" or "active context" instead of only "projection"
- "cleanup candidates" instead of only "segments to exclude"
- "shared" and "private" instead of internal storage terms
- "history preserved" where reversibility matters

Technical terms such as event, block, projection, provenance, and segment can
appear in details, docs, debug output, and advanced panels.

---

## 14. Context control: clean, tidy, compact, rewind

Context-control commands should form a coherent family.

### 14.1 `/clean`

Removes selected or semantically matched context from the live model-facing
projection while preserving event history.

Inputs:

```text
/clean "the failed attempt before we fixed config loading"
```

or visual selection from `/context`.

### 14.2 `/tidy`

Reorganizes and deduplicates context without silently summarizing away important
facts. This is a fast-follow feature, not required for the earliest V1.

### 14.3 `/compact`

Creates a derived summary or compressed representation. It is explicitly more
lossy than clean/tidy and should be presented as such.

### 14.4 `/rewind`

Restores a previous projection/checkpoint by appending a new event, not by
mutating history.

### 14.5 Cleanup safety

Cleanup should be conservative by default.

Every context mutation should show:

- what will change
- which context scope is affected
- token impact
- whether history is preserved
- undo or restoration path

In multi-agent sessions, cleanup must be explicit about scope:

- orchestrator context
- subagent private context
- shared session context
- model-facing working set

---

## 15. Session and model control

Session and model operations should reinforce the open substrate story.

### 15.1 `/model`

Model switches are visible transcript events and affect future turns. They should
not silently rewrite prior context.

### 15.2 `/clear`

Clearing starts a new session or new active projection. It should not delete the
old session.

### 15.3 `/resume`

Resuming should eventually restore:

- transcript
- model/provider
- context projection
- meter state
- subagent state if applicable
- previous decisions
- pending or completed tasks where supported

Historical replay and live rendering should use the same renderers.

---

## 16. Insights

Session insights are not required for the earliest TUI slice, but the UX should
reserve space for them.

Future `/insights` should help users answer:

- What burned tokens?
- Which files were re-read repeatedly?
- Which tool outputs were too large?
- Which commands were rediscovered?
- Which reusable project facts should be saved?
- Which subagents were effective?
- Which parts of the workflow caused retries or waste?
- What should be improved for next time?

Insights should be rendered as structured data and recommendations, not as a
long chat response.

Example direction:

```text
Insights · session "fix config loading"

Token waste
  - go test ./... output repeated 3 times        12.4k
  - docs/project/PRD.md read 4 times              5.6k

Reusable facts
  - test command: go test ./internal/config
  - config path: ~/.agent-smith/config.yaml

Subagent performance
  - Researcher found relevant docs quickly
  - Tester used broad test command too early

Suggested actions
  [Save test command to project memory]
  [Clean stale test outputs]
  [Open context panel]
```

---

## 17. Programmable interface requirements

Programmable use is first-class.

### 17.1 Output modes

Headless CLI should support at least:

- plain
- JSON
- stream JSON

Plain output is for humans and pipes.

JSON output is for scripts that want final structured results.

Stream JSON is for CI, dashboards, async runners, and future integrations that
want incremental events.

### 17.2 Exit codes

Headless CLI should define stable exit codes for:

- success
- task failure
- permission stop
- budget stop
- cancellation
- provider error
- internal error
- invalid command/input

### 17.3 No prompts by default

Headless mode should never hang waiting for an interactive permission decision.

It should use explicit policy:

- deny by default
- allowlist
- auto mode
- budget constraints
- explicit flags

### 17.4 Shared events

Stream JSON should come from the same event substrate used by the TUI.

The TUI may render:

```text
✓ shell go test ./...
```

while stream JSON emits a structured event representing the same tool result.

### 17.5 Command parity

Every built-in command should document whether it is:

- interactive only
- scriptable
- both
- planned scriptable later

Interactive-only commands need a reason.

---

## 18. Architecture boundaries for UX

### 18.1 Proposed layers

```text
cmd/smith
  wires configuration, providers, session controller, commands, and chosen face

internal/face
  face-agnostic event stream, command outputs, permission requests, render models

internal/tui
  Bubble Tea renderer/controller; no provider/tool/cost implementation ownership

internal/command
  slash command registry, parsing, output schemas, scriptability metadata

internal/session + internal/eventlog + internal/projection
  append-only truth and model-facing context projections

internal/loop + internal/tool + internal/provider
  agent execution and normalized provider/tool behavior
```

The exact package names may evolve. The important boundary is that the TUI is a
face, not the owner of business logic.

### 18.2 Render model recommendation

Introduce or evolve a face-neutral render model before tool transparency,
diff review, `/context`, and `/agents` become too large.

Candidate view models:

- `TranscriptItemView`
- `MessageView`
- `ToolCallView`
- `ToolResultView`
- `DiffView`
- `PermissionRequestView`
- `ContextSegmentView`
- `ContextDashboardView`
- `AgentView`
- `AgentTimelineView`
- `CommandOutputView`
- `SessionEventView`

The TUI renders these with Bubble Tea. Headless JSON outputs schemas derived from
the same concepts. Desktop/Web can later render the same models visually.

### 18.3 Event attribution recommendation

Events should support attribution from the beginning, even if V1 only has one
visible actor.

Sketch:

```go
type Event struct {
    ID        string
    SessionID string

    ActorID   string
    ActorRole string // user, orchestrator, researcher, implementer, tester, tool...
    ActorName string

    ParentID  string // optional task/tool/subagent relationship
    Type      string
    Data      any

    CreatedAt time.Time
}
```

This is illustrative, not a final schema. The key decision is that events should
not assume a single anonymous assistant.

### 18.4 Context ownership

Context segments should eventually be able to answer:

- who produced this?
- who currently depends on it?
- is it private to a subagent?
- is it shared with the orchestrator?
- is it in the model-facing working set?
- was it derived from other blocks?
- can it be cleaned without losing history?

This supports both TUI cleanup and future richer Desktop/Web visualizations.

---

## 19. Accessibility and terminal constraints

The TUI should remain useful without relying exclusively on color.

Guidelines:

- color helps, but symbols/text must carry meaning
- dangerous actions should be distinguishable without color
- long output should be navigable
- copy/paste should remain practical
- generated code and diffs should be easy to copy
- narrow terminals should degrade gracefully
- wide terminals may add side panels, not required core functionality

---

## 20. V1 UX scope

### 20.1 V1 should include

- stable transcript shell
- status line with model/context/cost/state
- multi-line input and slash command palette
- streaming assistant output
- first-class tool cards
- permission prompts
- batch diff review
- `/context` dashboard summary + table
- conservative `/clean`
- session/model control basics
- actor attribution in event/render model, even if only one actor is visible
- headless architecture compatibility, even if headless implementation follows

### 20.2 V1 should not require

- Desktop/Web
- full orchestration graph
- rich `/agents` panel
- per-hunk diff approval
- mouse support
- Matrix personality beyond small chrome
- `/insights`
- `/tidy`
- `/compact`
- automatic model routing
- collaboration
- advanced dashboards

---

## 21. Revisit later

These should remain explicit revisit items:

- stronger Matrix/personality mode
- mascot details
- mouse support
- wide-terminal side panel
- `/agents` panel
- orchestration graph/timeline
- per-file diff approval
- per-hunk diff approval
- Desktop app
- Web app
- collaboration/team workflows
- richer onboarding
- session insights
- living skills
- model routing/budgets UI
- cross-session dashboards

---

## 22. Locked decisions

1. Agent Smith is designed as a professional terminal cockpit, not a generic chat
   UI.
2. The TUI is the flagship interactive face, but core truth lives in the
   append-only event log and projections.
3. Visual richness should come from useful data views, especially context
   observability.
4. The default interface is transcript-first during work and dashboard-first
   during inspection.
5. V1 optimizes for a normal terminal and enhances on wide terminals later.
6. Personality is subtle, optional, and chrome-only.
7. Trust-critical surfaces may be themed, but must remain exact and serious.
8. Enter submits; Alt+Enter inserts a newline.
9. Panels are keyboard-first and inspired by lazygit/k9s interaction patterns.
10. `/context` opens as dashboard summary plus sortable table.
11. `/clean` supports both semantic command and visual selection.
12. Cleanup is conservative and reversible by projection/event design.
13. V1 diff review approves or rejects a full edit batch.
14. Subagent/orchestrator attribution must be present in the event/render model
    from the beginning.
15. `/agents` is not required in earliest V1, but the UX and architecture should
    reserve space for it.
16. Desktop/Web are not implemented now, but the core must not block richer
    visual dashboards, onboarding, review workflows, or collaboration later.
17. Headless/programmatic output is a first-class face and must remain free of
    personality and decoration by default.

---

## 23. Open questions

These are intentionally small and should not block the first UX implementation.

1. What exact package names should hold face-neutral view models?
2. How much subagent activity should be visible by default before it becomes
   noisy?
3. Should `/agents` ship before `/insights`, or only once orchestration becomes
   common?
4. What is the minimum useful JSON schema for stream output?
5. Should context cleanup preview be mandatory for all scopes or only risky
   scopes?
6. Should the startup mascot/art be enabled by default or only on first run/theme
   mode?
