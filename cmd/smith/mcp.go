package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/command"
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
	specs, warns, err := mcp.ConfigFrom(cfg)
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
	}
	registerMCPTools(reg, clients, stderr)
	return clients
}

// registerMCPTools registers the namespaced tools (and resource tools, AS-083) of
// already-connected MCP clients onto reg. It is split out of connectMCPServers so a
// delegated child (AS-119) can be given the same live tools over the parent's
// clients — borrowed, never re-dialled or closed by the child. A tool whose
// namespaced name collides is warned about and skipped (§7.4 isolation).
func registerMCPTools(reg *tool.Registry, clients []*mcp.Client, stderr io.Writer) {
	for _, client := range clients {
		for _, info := range client.Tools() {
			if err := reg.Register(mcpTool(client, info)); err != nil {
				_, _ = fmt.Fprintf(stderr, "warning: mcp: server %q tool %q: %v\n", client.Name(), info.Name, err)
			}
		}
		// A resource-capable server (AS-083) gets two synthetic tools so the model
		// can discover and pull resources into context, attributed per server like
		// any MCP output. Listing is on demand (no startup round trip).
		if client.HasResources() {
			for _, t := range mcpResourceTools(client) {
				if err := reg.Register(t); err != nil {
					_, _ = fmt.Fprintf(stderr, "warning: mcp: server %q resource tool: %v\n", client.Name(), err)
				}
			}
		}
	}
}

// closeMCPClients tears down every connected MCP client (and its subprocess).
func closeMCPClients(clients []*mcp.Client) {
	for _, c := range clients {
		_ = c.Close()
	}
}

// registerMCPCommands layers MCP slash commands (AS-083) over the built-ins after
// the command registry is built: a `/mcp` server-health-and-reconnect command, and
// one command per server prompt (`mcp__<server>__<prompt>`) that expands the prompt
// into a fresh user turn. Listing prompts is the one startup round trip MCP adds;
// a server whose prompt listing fails is skipped, never fatal (§7.4 isolation).
func registerMCPCommands(cmds *command.Registry, clients []*mcp.Client, stderr io.Writer) {
	if len(clients) == 0 {
		return
	}
	if err := cmds.Register(mcpStatusCommand(clients)); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: mcp: register /mcp: %v\n", err)
	}
	for _, c := range clients {
		if !c.HasPrompts() {
			continue
		}
		prompts, err := c.ListPrompts(context.Background())
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: mcp: server %q prompts: %v\n", c.Name(), err)
			continue
		}
		for _, p := range prompts {
			if err := cmds.Register(mcpPromptCommand(c, p)); err != nil {
				_, _ = fmt.Fprintf(stderr, "warning: mcp: server %q prompt %q: %v\n", c.Name(), p.Name, err)
			}
		}
	}
}

// mcpResourceTools builds the per-server resource tools: list_resources surfaces
// the catalog, read_resource pulls one into context as a file_read block sourced
// to mcp_resource and attributed to the server, so /context credits the read's
// cost to its origin. An unavailable server yields a model-readable error result
// rather than failing the turn (§7.4 isolation).
func mcpResourceTools(client *mcp.Client) []tool.Tool {
	server := client.Name()
	list := tool.Func{
		Spec: tool.Def{
			Name:        fmt.Sprintf("mcp__%s__list_resources", server),
			Description: fmt.Sprintf("List resources exposed by the %q MCP server. Returns each resource's uri (pass it to read_resource), name, and description.", server),
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		Fn: func(ctx context.Context, _ json.RawMessage) (tool.Output, error) {
			attr := &schema.Attribution{MCPServer: server, MCPTool: "list_resources"}
			res, err := client.ListResources(ctx)
			if err != nil {
				return tool.Output{Text: fmt.Sprintf("mcp server %q unavailable: %v", server, err), IsError: true, Attribution: attr}, nil
			}
			return tool.Output{Text: formatResourceList(res), Attribution: attr}, nil
		},
	}
	read := tool.Func{
		Spec: tool.Def{
			Name:        fmt.Sprintf("mcp__%s__read_resource", server),
			Description: fmt.Sprintf("Read a resource from the %q MCP server by its uri (obtained from list_resources).", server),
			InputSchema: json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI to read."}},"required":["uri"],"additionalProperties":false}`),
		},
		Fn: func(ctx context.Context, args json.RawMessage) (tool.Output, error) {
			attr := &schema.Attribution{MCPServer: server, MCPTool: "read_resource"}
			var in struct {
				URI string `json:"uri"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.URI) == "" {
				return tool.Output{Text: "read_resource requires a non-empty uri", IsError: true, Attribution: attr}, nil
			}
			content, err := client.ReadResource(ctx, in.URI)
			if err != nil {
				return tool.Output{Text: fmt.Sprintf("mcp server %q unavailable: %v", server, err), IsError: true, Attribution: attr}, nil
			}
			// The content rides in a file_read block (sourced mcp_resource, attributed
			// to the server) that /context can attribute and dedupe; the tool_result is
			// the loop-closer, mirroring the native read tool (AS-014).
			return tool.Output{
				Text:        content,
				Attribution: attr,
				FileRead: &schema.FileReadBody{
					Path:      in.URI,
					Content:   content,
					MediaType: "text",
					Source:    schema.FileSourceMCPResource,
				},
			}, nil
		},
	}
	return []tool.Tool{list, read}
}

// formatResourceList renders a resource catalog as one line per resource for the
// model, or a clear empty notice.
func formatResourceList(res []mcp.ResourceInfo) string {
	if len(res) == 0 {
		return "(no resources)"
	}
	lines := make([]string, 0, len(res))
	for _, r := range res {
		label := r.Name
		if label == "" {
			label = r.Title
		}
		line := r.URI
		if label != "" {
			line += " — " + label
		}
		if r.Description != "" {
			line += ": " + r.Description
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// mcpStatusCommand is `/mcp`: with no args it reports each server's health and
// catalog; `reconnect [server]` re-dials a crashed server so its tools recover
// without restarting the session (AS-083).
func mcpStatusCommand(clients []*mcp.Client) command.Command {
	return command.Command{
		Name:     "mcp",
		Summary:  "Show MCP server health; `reconnect [server]` re-dials a crashed server.",
		Args:     "[reconnect [server]]",
		Examples: []string{"/mcp", "/mcp reconnect", "/mcp reconnect github"},
		Run: func(ctx context.Context, args []string) (command.Output, error) {
			if len(args) > 0 && args[0] == "reconnect" {
				target := ""
				if len(args) > 1 {
					target = args[1]
				}
				return command.Output{Text: reconnectMCP(ctx, clients, target)}, nil
			}
			return command.Output{Text: mcpStatusText(clients)}, nil
		},
	}
}

// mcpStatusText renders one line per server: health, tool count, and which extra
// surfaces (resources/prompts) it offers.
func mcpStatusText(clients []*mcp.Client) string {
	lines := make([]string, 0, len(clients))
	for _, c := range clients {
		state := "healthy"
		if !c.Healthy() {
			state = "unavailable (try /mcp reconnect " + c.Name() + ")"
		}
		extras := []string{}
		if c.HasResources() {
			extras = append(extras, "resources")
		}
		if c.HasPrompts() {
			extras = append(extras, "prompts")
		}
		line := fmt.Sprintf("%s: %s, %d tool(s)", c.Name(), state, len(c.Tools()))
		if len(extras) > 0 {
			line += " [" + strings.Join(extras, ", ") + "]"
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// reconnectMCP re-dials the named server, or every unhealthy server when target is
// empty, and reports the outcome per server.
func reconnectMCP(ctx context.Context, clients []*mcp.Client, target string) string {
	var lines []string
	found := false
	for _, c := range clients {
		if target != "" && c.Name() != target {
			continue
		}
		found = true
		// A healthy server needs no re-dial. On a blanket reconnect skip it silently;
		// on a targeted one say so rather than falsely reporting "reconnected" (Reconnect
		// is a no-op that returns nil for a healthy server).
		if c.Healthy() {
			if target != "" {
				lines = append(lines, fmt.Sprintf("%s: already healthy", c.Name()))
			}
			continue
		}
		if err := c.Reconnect(ctx); err != nil {
			lines = append(lines, fmt.Sprintf("%s: reconnect failed: %v", c.Name(), err))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: reconnected, %d tool(s)", c.Name(), len(c.Tools())))
	}
	if len(lines) == 0 {
		if target != "" && !found {
			return fmt.Sprintf("no MCP server named %q", target)
		}
		return "no unavailable MCP servers to reconnect"
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// mcpPromptCommand wraps one server prompt as a slash command that expands into a
// fresh user turn (command.Output.Prompt). Positional args bind to the prompt's
// declared arguments in order, the last argument soaking up any remaining tokens
// so a free-text prompt argument works.
func mcpPromptCommand(c *mcp.Client, p mcp.PromptInfo) command.Command {
	return command.Command{
		Name:    fmt.Sprintf("mcp__%s__%s", c.Name(), p.Name),
		Summary: promptSummary(c.Name(), p),
		Args:    promptArgSpec(p.Arguments),
		Run: func(ctx context.Context, args []string) (command.Output, error) {
			text, err := c.GetPrompt(ctx, p.Name, bindPromptArgs(p.Arguments, args))
			if err != nil {
				return command.Output{}, fmt.Errorf("mcp prompt %q: %w", p.Name, err)
			}
			return command.Output{Prompt: text}, nil
		},
	}
}

// promptSummary labels a prompt command in the palette, falling back to the server
// name when the prompt carries no description.
func promptSummary(server string, p mcp.PromptInfo) string {
	if p.Description != "" {
		return p.Description
	}
	if p.Title != "" {
		return p.Title
	}
	return fmt.Sprintf("MCP prompt from %q", server)
}

// promptArgSpec renders a prompt's declared arguments as a help string, required
// ones in <angle> brackets and optional ones in [square] brackets.
func promptArgSpec(decls []mcp.PromptArgument) string {
	parts := make([]string, 0, len(decls))
	for _, d := range decls {
		if d.Required {
			parts = append(parts, "<"+d.Name+">")
		} else {
			parts = append(parts, "["+d.Name+"]")
		}
	}
	return strings.Join(parts, " ")
}

// bindPromptArgs maps positional command tokens onto a prompt's declared arguments
// in order; the final declared argument absorbs the remaining tokens so a
// free-text trailing argument is passed whole.
func bindPromptArgs(decls []mcp.PromptArgument, args []string) map[string]string {
	if len(decls) == 0 || len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(decls))
	for i, d := range decls {
		if i >= len(args) {
			break
		}
		if i == len(decls)-1 {
			out[d.Name] = strings.Join(args[i:], " ")
		} else {
			out[d.Name] = args[i]
		}
	}
	return out
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
