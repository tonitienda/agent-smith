# Containers (C4 level 2)

This view splits Agent Smith into runtime containers. Most production behavior is in one static Go binary; separate command packages provide repository maintenance utilities.

```mermaid
C4Container
    title Agent Smith - Containers

    Person(dev, "Developer")
    System_Ext(anthropic, "Anthropic API")
    System_Ext(openai, "OpenAI / compatible APIs")
    System_Ext(mcp, "MCP servers")
    System_Ext(hooks, "Hook commands")
    System_Ext(github, "GitHub")

    System_Boundary(repo, "agent-smith repository") {
        Container(smith, "smith binary", "Go CLI/TUI", "Single user-facing executable: cmd/smith is the process composition root, while internal/smithapp owns reusable app wiring.")
        Container(schema_guard, "schema-guard", "Go command", "Checks additive-only schema evolution for the open content-block schema.")
        Container(ticket_sync, "ticket-sync", "Go command", "Mirrors ticket markdown files to GitHub issues after merge.")
        ContainerDb(pricing, "Pricing data", "JSON", "Bundled model pricing table with optional user override.")
    }

    System_Boundary(local, "Local machine state") {
        ContainerDb(session_store, "Session store", "JSONL + JSON", "~/.agent-smith/sessions/<project-hash>/<session-id>/ events, metadata, debug logs.")
        ContainerDb(config_files, "Layered config", "JSON", "Defaults, env, user, project, and flag overrides merged by internal/config.")
        ContainerDb(project_assets, "Project assets", "Markdown / source files", "Memory files, skills, custom commands, and the project under edit.")
    }

    Rel(dev, smith, "Runs", "terminal")
    Rel(smith, session_store, "Append/replay sessions")
    Rel(smith, config_files, "Loads effective config")
    Rel(smith, project_assets, "Reads memory, skills, commands, source files; writes approved edits")
    Rel(smith, pricing, "Reads bundled rates")
    Rel(smith, anthropic, "Streams normalized turns", "HTTPS")
    Rel(smith, openai, "Streams normalized turns", "HTTPS")
    Rel(smith, mcp, "Registers/calls optional tools")
    Rel(smith, hooks, "Executes lifecycle hooks")
    Rel(schema_guard, project_assets, "Reads schema baselines")
    Rel(ticket_sync, project_assets, "Reads ticket files")
    Rel(ticket_sync, github, "Creates/updates/closes issues")
```

## Container inventory

| Container | Code | Criticality | Notes |
|---|---|---:|---|
| `smith` binary | `cmd/smith`, `internal/smithapp`, `internal/*`, `schema` | Critical | Main product. `cmd/smith` stays thin around process entry, streams, TTY detection, flags, and the command tree; reusable wiring for the router, provider/model, session, and built-in tools lives in `internal/smithapp`. The TUI, headless CLI, shared command handlers, agent loop, providers, tools, and storage remain in-process. See [Core components](core-components.md) for C4 level 3. |
| Session store | `internal/session`, `internal/eventlog` | Critical | Project-scoped durable JSONL event log plus small metadata. This is the audit/replay substrate. See [Core components](core-components.md). |
| Provider APIs | `internal/provider`, `internal/provider/anthropic`, `internal/provider/openai` | Critical | External systems, but critical to the harness boundary because all vendor wire formats normalize into one stream. See [Core components](core-components.md). |
| Tool/capability integrations | `internal/tool`, `internal/mcp`, `internal/hook`, `internal/skill`, `internal/customcmd`, `internal/memory` | Important | Extension surface for local action and reusable context. Covered at component level because tools are central to safety and observability. |
| `schema-guard` | `cmd/schema-guard`, `internal/schemaguard` | Supporting | Repository utility for additive schema discipline; level 2 is enough. |
| `ticket-sync` | `cmd/ticket-sync` | Supporting | Project-management automation; level 2 is enough. |
| Pricing data | `internal/cost/data/pricing.json` | Supporting | Data file used by cost reporting and budget checks; level 2 is enough. |
