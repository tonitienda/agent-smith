package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/mcp"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// connectMCPServers reads the `mcp.servers` config (AS-036), connects each server,
// and registers its tools into reg under namespaced names (`mcp__<server>__<tool>`)
// so MCP calls flow through permissions, logging, and /context attribution like
// native tools. A server that fails to connect, or a tool whose namespaced name
// collides, is warned about and skipped — one broken server never aborts the
// session (§7.4 isolation). It returns the connected clients so the caller can
// Close them (and reap any subprocesses) at session end; a nil/empty result needs
// no special-casing.
func connectMCPServers(ctx context.Context, cfg *config.Config, reg *tool.Registry, stderr io.Writer) []*mcp.Client {
	specs, warns, err := mcp.Parse(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: mcp: %v\n", err)
		return nil
	}
	for _, w := range warns {
		_, _ = fmt.Fprintf(stderr, "warning: config: %s\n", w)
	}
	var clients []*mcp.Client
	for _, spec := range specs {
		client, err := mcp.Dial(ctx, spec)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: mcp: server %q unavailable: %v\n", spec.Name, err)
			continue
		}
		clients = append(clients, client)
		for _, info := range client.Tools() {
			if err := reg.Register(mcpTool(client, info)); err != nil {
				_, _ = fmt.Fprintf(stderr, "warning: mcp: server %q tool %q: %v\n", spec.Name, info.Name, err)
			}
		}
	}
	return clients
}

// closeMCPClients tears down every connected MCP client (and its subprocess).
func closeMCPClients(clients []*mcp.Client) {
	for _, c := range clients {
		_ = c.Close()
	}
}

// mcpTool adapts one MCP server tool into a runtime Tool. Its model-facing name is
// namespaced `mcp__<server>__<tool>`; Run calls the server and attributes the
// result to the server/tool so /context credits MCP output per server (AS-036).
// An unavailable server (crashed, hung, never connected) yields a model-readable
// error result rather than failing the turn, keeping the session healthy.
func mcpTool(client *mcp.Client, info mcp.ToolInfo) tool.Tool {
	name := fmt.Sprintf("mcp__%s__%s", client.Name(), info.Name)
	return tool.Func{
		Spec: tool.Def{
			Name:        name,
			Description: info.Description,
			InputSchema: info.InputSchema,
		},
		Fn: func(ctx context.Context, args json.RawMessage) (tool.Output, error) {
			// Attribute every result — success or unavailable error — to the server/tool
			// so /context credits MCP cost per server even for failed calls.
			attr := &schema.Attribution{MCPServer: client.Name(), MCPTool: info.Name}
			res, err := client.Call(ctx, info.Name, args)
			if err != nil {
				return tool.Output{
					Text:        fmt.Sprintf("mcp server %q unavailable: %v", client.Name(), err),
					IsError:     true,
					Attribution: attr,
				}, nil
			}
			return tool.Output{
				Text:        res.Text,
				IsError:     res.IsError,
				Attribution: attr,
			}, nil
		},
	}
}
