package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/mcp"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// TestMain lets the cmd/smith test binary double as a stdio MCP server when
// GO_MCP_MOCK=1, so the adapter can be exercised end-to-end against a real
// subprocess (connect, call, kill) without a separate helper binary.
func TestMain(m *testing.M) {
	if os.Getenv("GO_MCP_MOCK") == "1" {
		serveEchoMock(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// serveEchoMock answers the MCP handshake, advertises an "echo" tool, and echoes
// each call's arguments back as text.
func serveEchoMock(in io.Reader, out io.Writer) {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.ID == 0 {
			continue // notification
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			resp["result"] = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"resources": map[string]any{}, "prompts": map[string]any{}}}
		case "tools/list":
			resp["result"] = map[string]any{"tools": []any{
				map[string]any{"name": "echo", "description": "echo", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			var p struct {
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			resp["result"] = map[string]any{
				"content": []any{map[string]any{"type": "text", "text": string(p.Arguments)}},
			}
		case "resources/list":
			resp["result"] = map[string]any{"resources": []any{
				map[string]any{"uri": "mem://doc", "name": "doc", "description": "a doc"},
			}}
		case "resources/read":
			resp["result"] = map[string]any{"contents": []any{
				map[string]any{"uri": "mem://doc", "text": "resource body"},
			}}
		case "prompts/list":
			resp["result"] = map[string]any{"prompts": []any{
				map[string]any{"name": "hello", "description": "say hello", "arguments": []any{map[string]any{"name": "who", "required": true}}},
			}}
		case "prompts/get":
			resp["result"] = map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": "Hello, world"}},
			}}
		default:
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		_ = enc.Encode(resp)
	}
}

func callBlock(name, args string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindToolCall,
		Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{
			ToolUseID: "tu_" + name,
			Name:      name,
			Arguments: json.RawMessage(args),
		},
	}
}

func dialEcho(t *testing.T) *mcp.Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("no executable: %v", err)
	}
	c, err := mcp.Dial(context.Background(), mcp.ServerConfig{
		Name:    "echo-srv",
		Command: exe,
		Env:     map[string]string{"GO_MCP_MOCK": "1"},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestMCPToolAttributionAndNamespacing connects a real MCP subprocess, registers
// its tool, and runs a call through the real tool runtime: the recorded result is
// namespaced and attributed to the server/tool so /context credits it (AS-036).
func TestMCPToolAttributionAndNamespacing(t *testing.T) {
	client := dialEcho(t)
	tl := mcpTool(client, mcp.ToolInfo{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)})

	if got := tl.Def().Name; got != "mcp__echo-srv__echo" {
		t.Fatalf("namespaced name = %q", got)
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tl); err != nil {
		t.Fatalf("register: %v", err)
	}
	log := eventlog.New()
	rt := tool.NewRuntime(reg, log, tool.WithPermission(tool.AllowAll))

	res, err := rt.Execute(context.Background(), callBlock("mcp__echo-srv__echo", `{"hi":1}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Attribution == nil || res.Attribution.MCPServer != "echo-srv" || res.Attribution.MCPTool != "echo" {
		t.Fatalf("attribution = %+v, want MCP server/tool", res.Attribution)
	}
	if res.Attribution.Tool != "mcp__echo-srv__echo" {
		t.Fatalf("result should also carry the registered tool name, got %q", res.Attribution.Tool)
	}
	if !strings.Contains(resultText(res), `"hi":1`) {
		t.Fatalf("echo result text = %q", resultText(res))
	}
}

// TestMCPToolUnavailable proves a dead server degrades gracefully: the tool
// reports unavailable as a model-readable error, never an infra failure.
func TestMCPToolUnavailable(t *testing.T) {
	client := dialEcho(t)
	_ = client.Close() // kill the connection

	tl := mcpTool(client, mcp.ToolInfo{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)})
	out, err := tl.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run returned infra error: %v", err)
	}
	if !out.IsError || !strings.Contains(out.Text, "unavailable") {
		t.Fatalf("closed server should yield an unavailable error result, got %+v", out)
	}
}

// TestMCPResourceReadAttribution runs the synthetic read_resource tool through the
// real runtime: the content lands in a file_read block sourced mcp_resource and
// the read is attributed to the server, so /context credits the resource per
// server (AS-083).
func TestMCPResourceReadAttribution(t *testing.T) {
	client := dialEcho(t)
	if !client.HasResources() {
		t.Fatal("server should advertise resources")
	}
	tools := mcpResourceTools(client)
	if len(tools) != 2 {
		t.Fatalf("want list+read resource tools, got %d", len(tools))
	}
	reg := tool.NewRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("register %q: %v", tl.Def().Name, err)
		}
	}
	log := eventlog.New()
	rt := tool.NewRuntime(reg, log, tool.WithPermission(tool.AllowAll))

	res, err := rt.Execute(context.Background(), callBlock("mcp__echo-srv__read_resource", `{"uri":"mem://doc"}`))
	if err != nil {
		t.Fatalf("execute read_resource: %v", err)
	}
	if res.Attribution == nil || res.Attribution.MCPServer != "echo-srv" {
		t.Fatalf("tool_result attribution = %+v, want MCP server", res.Attribution)
	}
	var fr *schema.Block
	for _, b := range log.Events() {
		b := b
		if b.Kind == schema.KindFileRead {
			fr = &b
		}
	}
	if fr == nil {
		t.Fatal("read_resource should append a file_read block")
	}
	if fr.FileRead.Source != schema.FileSourceMCPResource {
		t.Fatalf("file_read source = %q, want %q", fr.FileRead.Source, schema.FileSourceMCPResource)
	}
	if fr.Attribution == nil || fr.Attribution.MCPServer != "echo-srv" {
		t.Fatalf("file_read attribution = %+v, want MCP server", fr.Attribution)
	}
	if fr.FileRead.Content != "resource body" {
		t.Fatalf("file_read content = %q", fr.FileRead.Content)
	}
}

// TestMCPPromptCommand wraps a server prompt as a slash command and confirms it
// expands into a fresh user turn (command.Output.Prompt).
func TestMCPPromptCommand(t *testing.T) {
	client := dialEcho(t)
	prompts, err := client.ListPrompts(context.Background())
	if err != nil || len(prompts) != 1 {
		t.Fatalf("list prompts: %v (%d)", err, len(prompts))
	}
	cmd := mcpPromptCommand(client, prompts[0])
	if cmd.Name != "mcp__echo-srv__hello" {
		t.Fatalf("prompt command name = %q", cmd.Name)
	}
	if cmd.Args != "<who>" {
		t.Fatalf("prompt arg spec = %q, want <who>", cmd.Args)
	}
	out, err := cmd.Run(context.Background(), []string{"world"})
	if err != nil {
		t.Fatalf("run prompt command: %v", err)
	}
	if out.Prompt != "Hello, world" {
		t.Fatalf("prompt expansion = %q, want submit-as-turn text", out.Prompt)
	}
}

// TestMCPStatusAndReconnect exercises the /mcp command's status and reconnect
// outputs against a live then closed server.
func TestMCPStatusAndReconnect(t *testing.T) {
	client := dialEcho(t)
	clients := []*mcp.Client{client}

	status := mcpStatusText(clients)
	if !strings.Contains(status, "echo-srv: healthy") || !strings.Contains(status, "resources") {
		t.Fatalf("status = %q", status)
	}

	// A blanket reconnect with all servers healthy is a no-op.
	if got := reconnectMCP(context.Background(), clients, ""); !strings.Contains(got, "no unavailable") {
		t.Fatalf("reconnect (all healthy) = %q", got)
	}
	// An unknown target names itself in the error.
	if got := reconnectMCP(context.Background(), clients, "nope"); !strings.Contains(got, "nope") {
		t.Fatalf("reconnect unknown = %q", got)
	}
	// A targeted reconnect of a healthy server says so, not "reconnected".
	if got := reconnectMCP(context.Background(), clients, "echo-srv"); !strings.Contains(got, "already healthy") {
		t.Fatalf("reconnect healthy target = %q", got)
	}
}

// resultText extracts the text of a tool_result block for assertions.
func resultText(b schema.Block) string {
	if b.ToolResult == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range b.ToolResult.Content {
		sb.WriteString(p.Text)
	}
	return sb.String()
}
