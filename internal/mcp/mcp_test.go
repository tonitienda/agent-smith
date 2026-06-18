package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary double as a mock stdio MCP server when
// GO_MCP_MOCK=1, so the subprocess (Dial/kill) path can be exercised against a
// real process without shipping a separate helper binary.
func TestMain(m *testing.M) {
	if os.Getenv("GO_MCP_MOCK") == "1" {
		serveMock(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// serveMock implements just enough of an MCP server for the tests: the handshake,
// a tools/list of {echo, boom, crash}, and tools/call where echo returns its
// arguments, boom reports a domain error, and crash exits the process.
func serveMock(in io.Reader, out io.Writer) {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.ID == 0 {
			continue // a notification (e.g. notifications/initialized): no reply
		}
		resp := rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"mock"}}`)
		case "tools/list":
			resp.Result = json.RawMessage(`{"tools":[` +
				`{"name":"echo","description":"echo args","inputSchema":{"type":"object"}},` +
				`{"name":"boom","description":"domain error"},` +
				`{"name":"crash","description":"kills the server"}]}`)
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			switch p.Name {
			case "echo":
				body, _ := json.Marshal(map[string]any{
					"content": []map[string]any{{"type": "text", "text": string(p.Arguments)}},
					"isError": false,
				})
				resp.Result = body
			case "boom":
				resp.Result = json.RawMessage(`{"content":[{"type":"text","text":"nope"}],"isError":true}`)
			case "crash":
				os.Exit(1)
			default:
				resp.Error = &rpcError{Code: -32601, Message: "unknown tool"}
			}
		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found"}
		}
		_ = enc.Encode(resp)
	}
}

// pipeClient builds a Client whose transport is wired to an in-process serveMock
// over pipes — no subprocess — and runs the handshake.
func pipeClient(t *testing.T) *Client {
	t.Helper()
	reqR, reqW := io.Pipe()   // client writes requests to reqW, server reads reqR
	respR, respW := io.Pipe() // server writes responses to respW, client reads respR
	go serveMock(reqR, respW)

	c := &Client{name: "mock", transport: newStdioTransport(reqW, respR, nil), tmo: 2 * time.Second}
	if err := c.handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	c.healthy = true
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestStdioHandshakeAndCall(t *testing.T) {
	c := pipeClient(t)

	tools := c.Tools()
	if len(tools) != 3 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want echo/boom/crash", tools)
	}

	res, err := c.Call(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}
	if !strings.Contains(res.Text, `"x":1`) || res.IsError {
		t.Fatalf("echo result = %+v", res)
	}

	res, err = c.Call(context.Background(), "boom", nil)
	if err != nil {
		t.Fatalf("call boom: %v", err)
	}
	if !res.IsError {
		t.Fatalf("boom should report a domain error: %+v", res)
	}
	if !c.Healthy() {
		t.Fatal("a domain error must not break the circuit")
	}
}

func TestStdioProcessConnectAndCrash(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("no executable path: %v", err)
	}
	c, err := Dial(context.Background(), ServerConfig{
		Name:    "proc",
		Command: exe,
		Env:     map[string]string{"GO_MCP_MOCK": "1"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial subprocess: %v", err)
	}
	defer func() { _ = c.Close() }()

	if got := len(c.Tools()); got != 3 {
		t.Fatalf("tools = %d, want 3", got)
	}

	// crash kills the server mid-call: the call fails and the circuit breaks, but
	// the session stays healthy (no panic), and the next call reports unavailable.
	if _, err := c.Call(context.Background(), "crash", nil); err == nil {
		t.Fatal("crash call should fail")
	}
	if c.Healthy() {
		t.Fatal("server should be unhealthy after a crash")
	}
	if _, err := c.Call(context.Background(), "echo", nil); err != ErrUnavailable {
		t.Fatalf("post-crash call err = %v, want ErrUnavailable", err)
	}
}

func TestStdioCallTimeout(t *testing.T) {
	// A server that connects but never answers a tools/call must not wedge the
	// caller: the per-call timeout returns an error and trips the circuit.
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()
	go func() {
		dec := json.NewDecoder(reqR)
		enc := json.NewEncoder(respW)
		for {
			var req rpcRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				_ = enc.Encode(rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Result: json.RawMessage(`{}`)})
			case "tools/list":
				_ = enc.Encode(rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Result: json.RawMessage(`{"tools":[]}`)})
			}
			// tools/call: deliberately never answered.
		}
	}()
	c := &Client{name: "slow", transport: newStdioTransport(reqW, respR, nil), tmo: 100 * time.Millisecond}
	if err := c.handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	c.healthy = true

	if _, err := c.Call(context.Background(), "echo", nil); err == nil {
		t.Fatal("slow call should time out")
	}
	if c.Healthy() {
		t.Fatal("a timed-out call should trip the circuit")
	}
}

func TestCallerCancelKeepsHealthy(t *testing.T) {
	// A server that connects but never answers a tools/call, so the only way out is
	// the caller's cancellation. Caller cancellation must NOT circuit-break the
	// server (it says nothing about server health), unlike our own per-call timeout.
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()
	go func() {
		dec := json.NewDecoder(reqR)
		enc := json.NewEncoder(respW)
		for {
			var req rpcRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				_ = enc.Encode(rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Result: json.RawMessage(`{}`)})
			case "tools/list":
				_ = enc.Encode(rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Result: json.RawMessage(`{"tools":[]}`)})
			}
			// tools/call: never answered.
		}
	}()
	c := &Client{name: "slow", transport: newStdioTransport(reqW, respR, nil), tmo: 10 * time.Second}
	if err := c.handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	c.healthy = true

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if _, err := c.Call(ctx, "echo", nil); err == nil {
		t.Fatal("cancelled call should error")
	}
	if !c.Healthy() {
		t.Fatal("caller cancellation must not circuit-break the server")
	}
}

func TestStdioWriteRespectsContext(t *testing.T) {
	// A peer that never reads our stdin: the synchronous pipe makes Encode block.
	// The write must still honor the deadline (the isolation guarantee) rather than
	// hang forever, and tear the connection down so nothing leaks.
	reqR, reqW := io.Pipe() // reqR is intentionally never read
	defer func() { _ = reqR.Close() }()
	respR, _ := io.Pipe()
	tr := newStdioTransport(reqW, respR, nil)
	defer func() { _ = tr.close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := tr.call(ctx, "initialize", map[string]any{}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a write to an unread pipe should fail at the deadline")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call did not return: a blocked write outlived its context")
	}
}

func TestHTTPTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == 0 { // a notification
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set(sessionHeader, "sess-1")
		switch req.Method {
		case "initialize":
			// Answer over SSE to exercise that response shape too.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+rpcJSON(req.ID, `{"protocolVersion":"2025-06-18"}`)+"\n\n")
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, rpcJSON(req.ID, `{"tools":[{"name":"ping","inputSchema":{"type":"object"}}]}`))
		case "tools/call":
			if r.Header.Get(sessionHeader) != "sess-1" {
				t.Errorf("missing session header on %s", req.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, rpcJSON(req.ID, `{"content":[{"type":"text","text":"pong"}]}`))
		}
	}))
	defer srv.Close()

	c, err := Dial(context.Background(), ServerConfig{Name: "remote", URL: srv.URL})
	if err != nil {
		t.Fatalf("dial http: %v", err)
	}
	defer func() { _ = c.Close() }()

	if tools := c.Tools(); len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("tools = %+v", c.Tools())
	}
	res, err := c.Call(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}
	if res.Text != "pong" {
		t.Fatalf("ping result = %q", res.Text)
	}
}

// rpcJSON renders a JSON-RPC success response with the given raw result.
func rpcJSON(id int, result string) string {
	b, _ := json.Marshal(rpcResponse{JSONRPC: jsonRPCVersion, ID: id, Result: json.RawMessage(result)})
	return string(b)
}
