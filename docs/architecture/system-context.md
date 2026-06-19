# System context (C4 level 1)

Agent Smith is a local, provider-agnostic coding-agent harness. It runs as a Go binary on a developer workstation, orchestrates model providers and tools, and persists project-scoped sessions as an open append-only data substrate.

```mermaid
C4Context
    title Agent Smith - System Context

    Person(dev, "Developer", "Runs interactive and headless coding-agent sessions.")
    Person(agent, "Coding agent / automation", "Invokes Smith in CI or scripts through the headless CLI.")

    System_Boundary(local, "Developer machine / project checkout") {
        System(smith, "Agent Smith", "Provider-agnostic coding-agent harness written in Go.")
        System_Ext(project, "Project files", "Source files, AGENTS.md / CLAUDE.md, .agent-smith config, custom commands, skills.")
        System_Ext(shell, "Local shell and filesystem", "Commands and file operations executed with the user's privileges after permission checks.")
        System_Ext(state, "Local Smith state", "Project-scoped session logs, metadata, debug logs under ~/.agent-smith.")
    }

    System_Ext(anthropic, "Anthropic API", "Claude model turns and streaming responses.")
    System_Ext(openai, "OpenAI / compatible APIs", "OpenAI model turns and compatible endpoint streams.")
    System_Ext(mcp, "MCP servers", "Optional external tools exposed over stdio or HTTP/SSE.")
    System_Ext(hooks, "Hook commands", "Operator-configured lifecycle commands that can observe, rewrite, or block actions.")
    System_Ext(github, "GitHub", "Ticket sync utility mirrors ticket files to GitHub issues.")

    Rel(dev, smith, "Uses", "TUI or CLI")
    Rel(agent, smith, "Automates", "headless CLI")
    Rel(smith, project, "Reads and writes project context")
    Rel(smith, shell, "Executes tools through permission gate")
    Rel(smith, state, "Persists and replays sessions")
    Rel(smith, anthropic, "Streams model turns", "HTTPS")
    Rel(smith, openai, "Streams model turns", "HTTPS")
    Rel(smith, mcp, "Discovers and calls tools", "stdio / HTTP/SSE")
    Rel(smith, hooks, "Runs lifecycle hooks", "subprocess")
    Rel(smith, github, "Syncs tickets", "GitHub API via cmd/ticket-sync")
```

## Architectural responsibilities

| Actor/system | Responsibility | Important constraints |
|---|---|---|
| Developer | Starts sessions, approves risky actions, inspects context and cost. | Smith is not an OS sandbox; it relies on user approval and local policy. |
| Agent Smith | Owns context projection, provider normalization, tool orchestration, permissions, cost/accounting, and local persistence. | Must keep session data append-only and schema evolution additive-only. |
| Model providers | Produce model output and tool calls through vendor-specific streaming APIs. | Provider details are normalized behind `internal/provider`. |
| Local tools | Read files, search, and run shell commands. | Tool execution is gated, bounded, logged, and projected like any other block. |
| Local state | Stores session metadata and JSONL event logs. | Event logs are durable audit artifacts and are never edited in place. |
| MCP servers and hooks | Extend Smith with optional tools and lifecycle automation. | Failures should degrade gracefully and remain visible. |
