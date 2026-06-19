# Runtime flows

These sequence diagrams complement the C4 views with the most important execution paths.

## Interactive turn

```mermaid
sequenceDiagram
    actor User
    participant TUI as TUI face
    participant Controller as chatSession controller
    participant Log as event log
    participant Projection as projection engine
    participant Loop as agent loop
    participant Provider as provider adapter/API
    participant Tools as tool runtime

    User->>TUI: Submit prompt
    TUI->>Controller: Run(ctx, prompt)
    Controller->>Log: Append user block
    Controller->>Loop: Run turn
    Loop->>Projection: Project(log events, target model)
    Projection-->>Loop: Live context blocks
    Loop->>Provider: Stream(request with context/tools)
    Provider-->>Loop: Text / reasoning / tool-call / usage events
    Loop->>Log: Append assistant and tool-call blocks
    alt model requests tools
        Loop->>Tools: ExecuteBatch(tool calls)
        Tools->>Tools: Validate args, permission gate, hooks, timeout, truncation
        Tools->>Log: Append linked tool_result blocks
        Tools-->>Loop: Tool results
        Loop->>Projection: Re-project updated log
        Loop->>Provider: Continue with tool results in context
    end
    Loop-->>Controller: Stop reason and usage events
    Controller-->>TUI: UI events, transcript updates, meter data
    TUI-->>User: Render response and status
```

## `/clean` preview and apply

```mermaid
sequenceDiagram
    actor User
    participant Command as /clean handler
    participant Projection as projection engine
    participant Clean as clean planner
    participant Log as event log

    User->>Command: /clean <handle>...
    Command->>Projection: Project current log
    Projection-->>Command: Live and excluded blocks
    Command->>Clean: Build removal plan
    Clean-->>Command: Preview: blocks, warnings, reclaimed tokens/cost
    Command-->>User: Show preview; wait for --apply/--cancel
    User->>Command: /clean --apply
    Command->>Log: Append exclusion event derived from target blocks
    Command->>Projection: Re-project log
    Projection-->>Command: Updated context with excluded blocks visible as excluded
    Command-->>User: Show applied summary
```

## Session resume

```mermaid
sequenceDiagram
    actor User
    participant CLI as CLI/TUI command
    participant Store as session store
    participant Log as event log
    participant Controller as controller
    participant Loop as agent loop

    User->>CLI: smith session resume <id> or /resume
    CLI->>Store: Open(project-scoped session id)
    Store->>Log: Replay events.jsonl
    Log-->>Store: In-memory log with monotonic sequence state
    Store-->>Controller: Session{metadata, log}
    Controller->>Loop: Rebuild engine over resumed log/provider/tools
    Loop-->>Controller: Ready for next projected turn
    Controller-->>User: Resume summary
```

## Headless `smith run`

```mermaid
sequenceDiagram
    actor Script
    participant CLI as CLI router
    participant Headless as headless runner
    participant Controller as session wiring
    participant Loop as agent loop
    participant Output as stdout/stderr renderer

    Script->>CLI: smith run "prompt" --output json|plain|stream-json
    CLI->>Headless: Parse prompt, config, output mode
    Headless->>Controller: Create/open session, providers, tools, hooks
    Controller->>Loop: Run prompt to stop condition
    Loop-->>Headless: Normalized UI events and final state
    Headless->>Output: Write result to stdout; diagnostics to stderr
```
