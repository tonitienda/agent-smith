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
			resp["result"] = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}}
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
