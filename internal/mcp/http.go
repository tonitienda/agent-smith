package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/tonitienda/agent-smith/internal/streamio"
)

// httpTransport speaks JSON-RPC over MCP's Streamable HTTP transport: every
// request is POSTed to a single endpoint, and the server answers with either a
// lone JSON object (Content-Type application/json) or an SSE stream
// (text/event-stream) carrying the response as a `data:` event. A server that
// issues a session ID on initialize (the Mcp-Session-Id header) has it echoed on
// every subsequent request. Each call is an independent HTTP round trip, so the
// transport is naturally safe for concurrent use; only the session ID is guarded.
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	mu        sync.Mutex
	nextID    int
	sessionID string
}

// sessionHeader is the Streamable HTTP transport's session-correlation header.
const sessionHeader = "Mcp-Session-Id"

// errorBodyLimit caps how much of an error or ignored response body the transport
// reads before giving up, so a misbehaving server cannot exhaust memory.
const errorBodyLimit = 4096

func newHTTPTransport(cfg ServerConfig) *httpTransport {
	return &httpTransport{
		url:     cfg.URL,
		headers: cfg.Headers,
		client:  &http.Client{},
	}
}

func (t *httpTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	t.mu.Unlock()

	resp, err := t.post(ctx, rpcRequest{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: raw})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if sid := resp.Header.Get(sessionHeader); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := streamio.ReadAllLimit(resp.Body, errorBodyLimit)
		return nil, fmt.Errorf("mcp: http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	rpc, err := readRPCResponse(resp, id)
	if err != nil {
		return nil, err
	}
	if rpc.Error != nil {
		return nil, rpc.Error
	}
	return rpc.Result, nil
}

func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	resp, err := t.post(ctx, rpcRequest{JSONRPC: jsonRPCVersion, Method: method, Params: raw})
	if err != nil {
		return err
	}
	status := resp.Status
	code := resp.StatusCode
	_ = streamio.DrainClose(resp.Body, errorBodyLimit)
	if code < 200 || code >= 300 {
		return fmt.Errorf("mcp: http %s", status)
	}
	return nil
}

// close is a no-op for the stateless HTTP transport: there is no persistent
// connection or process to reap.
func (t *httpTransport) close() error { return nil }

// post sends one JSON-RPC message to the endpoint with the MCP-required Accept
// header (both response shapes), the operator's custom headers, and the session
// ID once known.
func (t *httpTransport) post(ctx context.Context, msg rpcRequest) (*http.Response, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		req.Header.Set(sessionHeader, sid)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: http request: %w", err)
	}
	return resp, nil
}

// readRPCResponse extracts the JSON-RPC response from either response shape. For
// application/json it decodes the single object; for text/event-stream it scans
// `data:` events until it finds the one whose JSON-RPC ID matches the request,
// ignoring unrelated server messages on the stream.
func readRPCResponse(resp *http.Response, id int) (rpcResponse, error) {
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readSSEResponse(resp.Body, id)
	}
	var rpc rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return rpcResponse{}, fmt.Errorf("mcp: decode response: %w", err)
	}
	return rpc, nil
}

// readSSEResponse reads an SSE stream and returns the first JSON-RPC response
// whose ID matches id, ignoring unrelated server messages and non-JSON-RPC
// frames on the stream. SSE framing is shared with the provider adapters via
// internal/streamio; only the JSON-RPC ID correlation is MCP-specific.
func readSSEResponse(body io.Reader, id int) (rpcResponse, error) {
	r := streamio.NewSSEReader(body)
	for {
		data, err := r.ReadEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return rpcResponse{}, fmt.Errorf("mcp: read sse: %w", err)
		}
		var rpc rpcResponse
		if err := json.Unmarshal(data, &rpc); err != nil {
			continue // not a JSON-RPC frame; skip it
		}
		if rpc.ID == id {
			return rpc, nil
		}
	}
	return rpcResponse{}, fmt.Errorf("mcp: no response for request %d on sse stream", id)
}
