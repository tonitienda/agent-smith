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
    participant AgentLoop as Agent turn engine
    participant ProviderAdapter as provider adapter
    participant ToolRuntime as tool runtime

    User->>TUIFace: Submit prompt
    TUIFace->>ChatController: Run prompt
    ChatController->>EventLog: Append user block
    ChatController->>AgentLoop: Run turn
    AgentLoop->>ProjectionEngine: Project log events for target model
    ProjectionEngine-->>AgentLoop: Live context blocks
    AgentLoop->>ProviderAdapter: Stream request with context and tools
    ProviderAdapter-->>AgentLoop: Text reasoning tool call and usage events
    AgentLoop->>EventLog: Append assistant and tool call blocks
    alt model requests tools
        AgentLoop->>ToolRuntime: Execute tool calls
        ToolRuntime->>ToolRuntime: Validate gate run and truncate
        ToolRuntime->>EventLog: Append linked tool result blocks
        ToolRuntime-->>AgentLoop: Tool results
        AgentLoop->>ProjectionEngine: Project updated log
        AgentLoop->>ProviderAdapter: Continue with tool results in context
    end
    AgentLoop-->>ChatController: Stop reason and usage events
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

    User->>CleanCommand: Preview clean handles
    CleanCommand->>ProjectionEngine: Project current log
    ProjectionEngine-->>CleanCommand: Live and excluded blocks
    CleanCommand->>CleanPlanner: Build removal plan
    CleanPlanner-->>CleanCommand: Preview blocks warnings and reclaimed cost
    CleanCommand-->>User: Show preview and wait for confirmation
    User->>CleanCommand: Apply clean preview
    CleanCommand->>EventLog: Append exclusion event derived from target blocks
    CleanCommand->>ProjectionEngine: Project log
    ProjectionEngine-->>CleanCommand: Updated context with excluded blocks visible as excluded
    CleanCommand-->>User: Show applied summary
```

## Session resume

```mermaid
sequenceDiagram
    actor User
    participant CommandFace as CLI or TUI command
    participant SmithApp as internal/smithapp
    participant SessionStore as session store
    participant EventLog as event log
    participant ChatController as controller
    participant AgentLoop as Agent turn engine

    User->>CommandFace: Resume session by ID
    CommandFace->>SmithApp: OpenOrCreate with resume ID
    SmithApp->>SessionStore: Open project scoped session ID
    SessionStore->>EventLog: Replay events.jsonl
    EventLog-->>SessionStore: In memory log with monotonic sequence state
    SessionStore-->>SmithApp: Session metadata and log
    SmithApp-->>CommandFace: Resumed session
    CommandFace->>ChatController: Initialize with resumed session
    ChatController->>AgentLoop: Rebuild engine over resumed session wiring
    AgentLoop-->>ChatController: Ready for next projected turn
    ChatController-->>User: Resume summary
```

## Headless `smith run`

```mermaid
sequenceDiagram
    actor Script
    participant ProcessRoot as cmd/smith process root
    participant SmithApp as internal/smithapp
    participant CLIRouter as CLI router
    participant HeadlessRunner as headless runner
    participant ChatController as session wiring
    participant AgentLoop as Agent turn engine
    participant OutputRenderer as output renderer

    Script->>ProcessRoot: Start smith run with a prompt
    ProcessRoot->>SmithApp: BuildCLI with streams, env, bare handler, commands
    SmithApp-->>ProcessRoot: Face-neutral CLI app
    ProcessRoot->>CLIRouter: Dispatch argv
    CLIRouter->>HeadlessRunner: Parse prompt config and output mode
    HeadlessRunner->>SmithApp: Resolve runtime defaults (providers, model, tools, session)
    SmithApp-->>HeadlessRunner: Runtime defaults
    HeadlessRunner->>ChatController: Combine runtime defaults with config, permissions, hooks, MCP
    ChatController-->>HeadlessRunner: Wired session and loop engine
    ChatController->>AgentLoop: Run prompt to stop condition
    AgentLoop-->>HeadlessRunner: Normalized UI events and final state
    HeadlessRunner->>OutputRenderer: Write result to stdout and diagnostics to stderr
```

## `smith serve` turn over JSON-RPC/WebSocket

The `internal/serve` face exposes the same face-agnostic loop to programmatic
clients (the web GUI AS-078, the Viscose extension AS-081). Client→server calls
are JSON-RPC requests (`session.start`, `turn.run`, `turn.cancel`,
`session.list`); turn output streams back as server-initiated `event`
notifications; an ask-mode permission prompt (AS-016) is a server-initiated
`permission.ask` request that blocks the turn until the client answers, and
fails fast if it cannot.

```mermaid
sequenceDiagram
    actor Client as WebSocket client
    participant Conn as serve conn
    participant Backend as Backend (cmd/smith)
    participant Session as serve session
    participant AgentLoop as Agent turn engine
    participant ToolRuntime as tool runtime

    Client->>Conn: session.start (optional resume_id)
    Conn->>Backend: Open(resumeID, conn)
    Backend-->>Conn: Session bound to this conn
    Conn-->>Client: reply session_id

    Client->>Conn: turn.run (prompt)
    Conn->>Session: Run(ctx, prompt) off the read loop
    Session->>AgentLoop: Run turn over wired session
    AgentLoop-->>Session: UI events (text, tool calls, usage)
    Session->>Conn: Emit each UI event
    Conn--)Client: event notification (per UI event)
    alt tool call needs interactive approval
        AgentLoop->>ToolRuntime: Execute gated tool call
        ToolRuntime->>Session: Ask permission
        Session->>Conn: AskPermission(req)
        Conn->>Client: permission.ask request (server-initiated)
        alt client answers
            Client-->>Conn: permission decision
            Conn-->>ToolRuntime: Decision (allow/deny)
        else client cannot answer
            Conn-->>ToolRuntime: Fail-fast deny
        end
        ToolRuntime-->>AgentLoop: Tool result or denial
    end
    AgentLoop-->>Session: Stop reason and final state
    Session-->>Conn: Turn result
    Conn-->>Client: reply turn.run result
```
