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
// Beyond tools (AS-083, §7.4) the client also lists and reads resources, lists and
// expands prompts, follows tools/list pagination so a paged catalog exposes every
// tool, and re-dials a circuit-broken server on demand (Reconnect) so a restarted
// server's tools recover without restarting the session. Resources and prompts
// honour the same isolation contract as tools.
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

// reconnectMinInterval throttles re-dials of a still-dead server: a Reconnect is
// refused if the last attempt was more recent than this, so repeatedly poking a
// crashed server can't relaunch its subprocess on a tight loop.
const reconnectMinInterval = 3 * time.Second

// serverCaps records the optional capabilities a server advertised on initialize,
// so the client only surfaces resources/prompts for servers that support them.
type serverCaps struct {
	resources bool
	prompts   bool
}

// Client is a live connection to one MCP server. cfg is retained so a
// circuit-broken connection can be re-dialed (Reconnect); transport, healthy,
// tools, and caps are all guarded by mu because Reconnect swaps the transport and
// refreshes the catalog while concurrent calls (AS-019) read them.
type Client struct {
	name string
	cfg  ServerConfig
	tmo  time.Duration

	dialMu sync.Mutex // serializes Reconnect so a dead server is re-dialed once at a time

	mu        sync.Mutex
	transport transport
	healthy   bool
	tools     []ToolInfo
	caps      serverCaps
	lastDial  time.Time // last Reconnect attempt, to throttle re-dials of a still-dead server
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
	c := &Client{name: cfg.Name, cfg: cfg, transport: tr, tmo: cfg.timeout()}
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
// transport failure (the circuit break) and stays false until a successful
// Reconnect (AS-083) restores it.
func (c *Client) Healthy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.healthy
}

// HasResources reports whether the server advertised the resources capability.
func (c *Client) HasResources() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps.resources
}

// HasPrompts reports whether the server advertised the prompts capability.
func (c *Client) HasPrompts() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps.prompts
}

// Close tears down the connection (and its subprocess, for stdio).
func (c *Client) Close() error {
	c.mu.Lock()
	tr := c.transport
	c.mu.Unlock()
	return tr.close()
}

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
	raw, err := c.transport.call(ctx, "initialize", initParams)
	if err != nil {
		return fmt.Errorf("mcp: initialize %q: %w", c.name, err)
	}
	c.caps = parseCaps(raw)
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

// parseCaps reads the optional capability flags off an initialize result. A
// capability is "present" when its (possibly empty) object is sent; a missing key
// means the server does not offer it, so the client never probes resources/prompts
// on a server that lacks them.
func parseCaps(raw json.RawMessage) serverCaps {
	var out struct {
		Capabilities struct {
			Resources *json.RawMessage `json:"resources"`
			Prompts   *json.RawMessage `json:"prompts"`
		} `json:"capabilities"`
	}
	_ = json.Unmarshal(raw, &out) // a malformed result simply yields no capabilities
	return serverCaps{
		resources: out.Capabilities.Resources != nil,
		prompts:   out.Capabilities.Prompts != nil,
	}
}

// listTools requests the server's tool catalog, following nextCursor so a server
// that pages its catalog exposes every tool (AS-083). It runs on the transport
// directly because it is part of the handshake, before the client is marked
// healthy. It is a fatal handshake error if any page fails.
func (c *Client) listTools(ctx context.Context) ([]ToolInfo, error) {
	var tools []ToolInfo
	err := pageThrough(func(params map[string]any) (string, error) {
		raw, err := c.transport.call(ctx, "tools/list", params)
		if err != nil {
			return "", err
		}
		var out struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", fmt.Errorf("decode tools: %w", err)
		}
		for _, t := range out.Tools {
			if t.Name == "" {
				continue
			}
			tools = append(tools, ToolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
		}
		return out.NextCursor, nil
	})
	return tools, err
}

// Reconnect re-dials a circuit-broken server: it builds a fresh transport from the
// stored config, re-runs the handshake, and atomically swaps it in so the server's
// tools (and resources/prompts) recover without restarting the session (AS-083). A
// healthy server is left untouched. Concurrent callers are serialized; only the
// first re-dials and the rest observe the restored connection. Re-dials of a
// still-dead server are throttled (reconnectMinInterval) so retries can't relaunch
// a crashed subprocess on a tight loop.
func (c *Client) Reconnect(parent context.Context) error {
	c.dialMu.Lock()
	defer c.dialMu.Unlock()

	c.mu.Lock()
	if c.healthy { // another caller already restored it
		c.mu.Unlock()
		return nil
	}
	if !c.lastDial.IsZero() && time.Since(c.lastDial) < reconnectMinInterval {
		c.mu.Unlock()
		return fmt.Errorf("mcp: %q reconnect throttled", c.name)
	}
	c.lastDial = time.Now()
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, c.tmo)
	defer cancel()

	tr, err := newTransport(c.cfg)
	if err != nil {
		return fmt.Errorf("mcp: %q reconnect: %w", c.name, err)
	}
	probe := &Client{name: c.name, cfg: c.cfg, transport: tr, tmo: c.tmo}
	if err := probe.handshake(ctx); err != nil {
		_ = tr.close()
		return fmt.Errorf("mcp: %q reconnect: %w", c.name, err)
	}

	c.mu.Lock()
	old := c.transport
	c.transport = tr
	c.tools = probe.tools
	c.caps = probe.caps
	c.healthy = true
	c.mu.Unlock()
	_ = old.close() // reap the dead connection/subprocess
	return nil
}

// Call invokes a tool on the server. args is the model's JSON arguments object.
// A transport failure (dead or hung server) marks the server unhealthy and is
// returned as an error for the adapter to render as unavailable; a server
// protocol error or a tool's own domain error comes back without breaking the
// circuit (the latter as a CallResult with IsError set).
func (c *Client) Call(parent context.Context, toolName string, args json.RawMessage) (CallResult, error) {
	params := map[string]any{"name": toolName}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	} else {
		params["arguments"] = map[string]any{}
	}
	raw, err := c.rpc(parent, "tools/call", params)
	if err != nil {
		return CallResult{}, err
	}
	return parseCallResult(raw)
}

// rpc performs one request against the live transport under the call timeout,
// applying the §7.4 isolation contract uniformly to every method (tools, resources,
// prompts): an unhealthy server short-circuits to ErrUnavailable, and a real
// transport fault trips the circuit break while a protocol error or caller
// cancellation leaves the connection healthy. The transport is snapshotted under
// mu so a concurrent Reconnect swap is race-free.
func (c *Client) rpc(parent context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	healthy, tr := c.healthy, c.transport
	c.mu.Unlock()
	if !healthy {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, c.tmo)
	defer cancel()
	raw, err := tr.call(ctx, method, params)
	if err != nil {
		if isTransportFault(parent, err) {
			c.markUnhealthy() // a real transport fault trips the circuit break
		}
		return nil, err
	}
	return raw, nil
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
