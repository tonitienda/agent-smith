// Package mcp is a client for Model Context Protocol servers (AS-036, PRD §7.4):
// it connects to a stdio (subprocess) or HTTP/SSE server, performs the MCP
// handshake, lists the server's tools, and calls them on the model's behalf. It
// is face- and runtime-agnostic and depends only on the stdlib — it returns plain
// ToolInfo descriptors and a Client with a Call method; the wiring (cmd/smith)
// adapts each MCP tool into the tool runtime (AS-013) under a namespaced name
// (`mcp__<server>__<tool>`) so MCP calls flow through permissions (AS-016),
// logging, and `/context` attribution exactly like native tools. This mirrors how
// the skill package stays runtime-agnostic and cmd/smith builds the "skill" tool.
//
// Isolation is the design center (§7.4): a crashing, hanging, or slow server must
// degrade only its own tools, never the session. Every call runs under a timeout,
// any transport failure marks the server unhealthy (a circuit break), and an
// unhealthy server's tools report unavailable immediately instead of blocking the
// loop. A server-reported protocol error or a tool's own domain error is passed
// back to the model and leaves the connection healthy.
//
// Scope is tools. Resources and prompts (§7.4) and on-demand reconnect are a
// documented follow-on (AS-083), not silently dropped (PRD D0).
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// protocolVersion is the MCP revision the client advertises on initialize.
const protocolVersion = "2025-06-18"

// DefaultTimeout bounds a single MCP call when a server config sets none, so a
// hung server cannot wedge the turn.
const DefaultTimeout = 30 * time.Second

// errConnClosed is the transport-level signal that a server connection is gone.
var errConnClosed = errors.New("mcp: server connection closed")

// ErrUnavailable is returned by Call when the server is unhealthy (it crashed,
// hung, or never connected). The tool adapter turns it into a model-readable
// "unavailable" result so the loop continues with the session intact.
var ErrUnavailable = errors.New("mcp: server unavailable")

// ServerConfig describes one MCP server from config (AS-031, `mcp.servers`).
// URL selects the HTTP/SSE transport; otherwise Command selects stdio. Name is
// the server's key, used to namespace its tools.
type ServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	Timeout time.Duration
}

func (c ServerConfig) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// ToolInfo is a tool advertised by a server: its name (unqualified — the caller
// adds the `mcp__<server>__` prefix), model-facing description, and JSON-Schema
// arguments object passed through verbatim to the model and the runtime.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// CallResult is a flattened tools/call result: the text content joined from the
// server's content parts, and whether the server marked the call a domain error.
type CallResult struct {
	Text    string
	IsError bool
}

// Client is a live connection to one MCP server.
type Client struct {
	name      string
	transport transport
	tmo       time.Duration

	mu      sync.Mutex
	healthy bool
	tools   []ToolInfo
}

// Dial connects to the server described by cfg, runs the MCP handshake, and lists
// its tools. A failure to connect or hand-shake returns an error (and tears down
// any transport); the caller degrades that one server rather than aborting the
// session.
func Dial(ctx context.Context, cfg ServerConfig) (*Client, error) {
	tr, err := newTransport(cfg)
	if err != nil {
		return nil, err
	}
	c := &Client{name: cfg.Name, transport: tr, tmo: cfg.timeout()}
	if err := c.handshake(ctx); err != nil {
		_ = tr.close()
		return nil, err
	}
	c.healthy = true
	return c, nil
}

// newTransport selects the transport from the config shape: URL → HTTP/SSE,
// else Command → stdio subprocess.
func newTransport(cfg ServerConfig) (transport, error) {
	switch {
	case cfg.URL != "":
		return newHTTPTransport(cfg), nil
	case cfg.Command != "":
		return newProcessTransport(cfg)
	default:
		return nil, fmt.Errorf("mcp: server %q has neither command nor url", cfg.Name)
	}
}

// Name reports the server's configured name.
func (c *Client) Name() string { return c.name }

// Tools returns a copy of the server's advertised tools, in server order.
func (c *Client) Tools() []ToolInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ToolInfo(nil), c.tools...)
}

// Healthy reports whether the server is still usable. It goes false on the first
// transport failure (the circuit break) and never recovers within this session.
func (c *Client) Healthy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.healthy
}

// Close tears down the connection (and its subprocess, for stdio).
func (c *Client) Close() error { return c.transport.close() }

// handshake performs the initialize request, the initialized notification, and
// the initial tools/list, all under the call timeout.
func (c *Client) handshake(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.tmo)
	defer cancel()

	initParams := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agent-smith"},
	}
	if _, err := c.transport.call(ctx, "initialize", initParams); err != nil {
		return fmt.Errorf("mcp: initialize %q: %w", c.name, err)
	}
	if err := c.transport.notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("mcp: initialized %q: %w", c.name, err)
	}
	tools, err := c.listTools(ctx)
	if err != nil {
		return fmt.Errorf("mcp: list tools %q: %w", c.name, err)
	}
	c.tools = tools
	return nil
}

// listTools requests the server's tool catalog. Pagination (nextCursor) is not
// followed — the first page is taken — which covers the common case; full
// pagination is part of the AS-083 follow-on.
func (c *Client) listTools(ctx context.Context) ([]ToolInfo, error) {
	raw, err := c.transport.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	tools := make([]ToolInfo, 0, len(out.Tools))
	for _, t := range out.Tools {
		if t.Name == "" {
			continue
		}
		tools = append(tools, ToolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return tools, nil
}

// Call invokes a tool on the server. args is the model's JSON arguments object.
// A transport failure (dead or hung server) marks the server unhealthy and is
// returned as an error for the adapter to render as unavailable; a server
// protocol error or a tool's own domain error comes back without breaking the
// circuit (the latter as a CallResult with IsError set).
func (c *Client) Call(parent context.Context, toolName string, args json.RawMessage) (CallResult, error) {
	if !c.Healthy() {
		return CallResult{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, c.tmo)
	defer cancel()

	params := map[string]any{"name": toolName}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	} else {
		params["arguments"] = map[string]any{}
	}
	raw, err := c.transport.call(ctx, "tools/call", params)
	if err != nil {
		if isTransportFault(parent, err) {
			c.markUnhealthy() // a real transport fault trips the circuit break
		}
		return CallResult{}, err
	}
	return parseCallResult(raw)
}

// isTransportFault reports whether a Call error reflects an unhealthy server and
// should trip the circuit break. A protocol-level rpcError (the server answered,
// just with an error) does not, and neither does a caller-initiated cancellation
// or deadline (parent.Err() != nil) — that says nothing about server health and
// must not permanently disable the server for the rest of the session. Our own
// per-call timeout (a hung server: parent still live, ctx deadline exceeded) is
// left as a fault, which is exactly the hang we want to break the circuit on.
func isTransportFault(parent context.Context, err error) bool {
	var rerr *rpcError
	if errors.As(err, &rerr) {
		return false
	}
	return parent.Err() == nil
}

func (c *Client) markUnhealthy() {
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()
}

// parseCallResult flattens a tools/call result into text. MCP content is a list
// of typed parts; text parts are joined with newlines and any non-text part is
// noted in place so the model knows content was elided rather than missing.
func parseCallResult(raw json.RawMessage) (CallResult, error) {
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return CallResult{}, fmt.Errorf("mcp: decode call result: %w", err)
	}
	var parts []string
	for _, p := range out.Content {
		if p.Type == "text" {
			parts = append(parts, p.Text)
		} else {
			parts = append(parts, fmt.Sprintf("[%s content omitted]", p.Type))
		}
	}
	return CallResult{Text: joinLines(parts), IsError: out.IsError}, nil
}

func joinLines(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		s := parts[0]
		for _, p := range parts[1:] {
			s += "\n" + p
		}
		return s
	}
}
