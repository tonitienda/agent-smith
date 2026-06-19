# Runtime flows

These sequence diagrams complement the C4 views with the most important execution paths.

## Interactive turn

```mermaid
sequenceDiagram
    actor User
    participant TUIFace as TUI face
    participant ChatController as chatSession controller
    participant EventLog as event log
    participant ProjectionEngine as projection engine
    participant AgentEngine as agent engine
    participant ProviderAdapter as provider adapter/API
    participant ToolRuntime as tool runtime

    User->>TUIFace: Submit prompt
    TUIFace->>ChatController: Run(ctx, prompt)
    ChatController->>EventLog: Append user block
    ChatController->>AgentEngine: Run turn
    AgentEngine->>ProjectionEngine: Project(log events, target model)
    ProjectionEngine-->>AgentEngine: Live context blocks
    AgentEngine->>ProviderAdapter: Stream(request with context/tools)
    ProviderAdapter-->>AgentEngine: Text / reasoning / tool-call / usage events
    AgentEngine->>EventLog: Append assistant and tool-call blocks
    alt model requests tools
        AgentEngine->>ToolRuntime: ExecuteBatch(tool calls)
        ToolRuntime->>ToolRuntime: Validate args, permission gate, hooks, timeout, truncation
        ToolRuntime->>EventLog: Append linked tool_result blocks
        ToolRuntime-->>AgentEngine: Tool results
        AgentEngine->>ProjectionEngine: Re-project updated log
        AgentEngine->>ProviderAdapter: Continue with tool results in context
    end
    AgentEngine-->>ChatController: Stop reason and usage events
    ChatController-->>TUIFace: UI events, transcript updates, meter data
    TUIFace-->>User: Render response and status
```

## `/clean` preview and apply

```mermaid
sequenceDiagram
    actor User
    participant CleanCommand as clean command handler
    participant ProjectionEngine as projection engine
    participant CleanPlanner as clean planner
    participant EventLog as event log

    User->>CleanCommand: /clean <handle>...
    CleanCommand->>ProjectionEngine: Project current log
    ProjectionEngine-->>CleanCommand: Live and excluded blocks
    CleanCommand->>CleanPlanner: Build removal plan
    CleanPlanner-->>CleanCommand: Preview: blocks, warnings, reclaimed tokens/cost
    CleanCommand-->>User: Show preview; wait for --apply/--cancel
    User->>CleanCommand: /clean --apply
    CleanCommand->>EventLog: Append exclusion event derived from target blocks
    CleanCommand->>ProjectionEngine: Re-project log
    ProjectionEngine-->>CleanCommand: Updated context with excluded blocks visible as excluded
    CleanCommand-->>User: Show applied summary
```

## Session resume

```mermaid
sequenceDiagram
    actor User
    participant CommandFace as CLI or TUI command
    participant SessionStore as session store
    participant EventLog as event log
    participant ChatController as controller
    participant AgentEngine as agent engine

    User->>CommandFace: smith session resume ID or /resume
    CommandFace->>SessionStore: Open(project-scoped session id)
    SessionStore->>EventLog: Replay events.jsonl
    EventLog-->>SessionStore: In-memory log with monotonic sequence state
    SessionStore-->>ChatController: Session metadata and log
    ChatController->>AgentEngine: Rebuild engine over resumed log/provider/tools
    AgentEngine-->>ChatController: Ready for next projected turn
    ChatController-->>User: Resume summary
```

## Headless `smith run`

```mermaid
sequenceDiagram
    actor Script
    participant CLIRouter as CLI router
    participant HeadlessRunner as headless runner
    participant ChatController as session wiring
    participant AgentEngine as agent engine
    participant OutputRenderer as stdout and stderr renderer

    Script->>CLIRouter: smith run "prompt" --output json|plain|stream-json
    CLIRouter->>HeadlessRunner: Parse prompt, config, output mode
    HeadlessRunner->>ChatController: Create/open session, providers, tools, hooks
    ChatController->>AgentEngine: Run prompt to stop condition
    AgentEngine-->>HeadlessRunner: Normalized UI events and final state
    HeadlessRunner->>OutputRenderer: Write result to stdout; diagnostics to stderr
```
