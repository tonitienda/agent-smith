package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// stdioTransport speaks newline-delimited JSON-RPC over a pair of streams — an
// MCP server's stdin (we write requests) and stdout (we read responses). A single
// reader goroutine decodes responses and hands each to the goroutine waiting on
// its request ID, so concurrent calls (AS-019) are correlated without a lock held
// across the round trip. Any read failure (the server died or closed its stdout)
// fails every in-flight and future call, which the Client treats as the server
// going unavailable rather than wedging the session (AS-036 isolation).
type stdioTransport struct {
	mu      sync.Mutex
	writeMu sync.Mutex // serializes Encode, held without mu so a blocked write can't wedge the reader
	enc     *json.Encoder
	w       io.Closer
	nextID  int
	pending map[int]chan rpcResponse
	closed  chan struct{}
	onClose func() error // tears down the underlying process, if any
}

// newStdioTransport wires a transport over an already-open stdin/stdout pair and
// starts its reader. onClose, if non-nil, runs on close to reap the backing
// process. Decoupling the streams from the process makes the transport testable
// with in-memory pipes.
func newStdioTransport(stdin io.WriteCloser, stdout io.Reader, onClose func() error) *stdioTransport {
	t := &stdioTransport{
		enc:     json.NewEncoder(stdin),
		w:       stdin,
		pending: map[int]chan rpcResponse{},
		closed:  make(chan struct{}),
		onClose: onClose,
	}
	go t.readLoop(stdout)
	return t
}

// newProcessTransport launches an MCP server subprocess and speaks to it over its
// stdio (the §7.4 stdio transport). The server's stderr is forwarded to ours so
// its diagnostics stay visible; close kills and reaps the process.
func newProcessTransport(cfg ServerConfig) (*stdioTransport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec // operator-configured MCP server command (AS-031)
	cmd.Env = append(os.Environ(), envSlice(cfg.Env)...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", cfg.Command, err)
	}
	return newStdioTransport(stdin, stdout, func() error {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil
	}), nil
}

// readLoop decodes successive JSON-RPC responses off stdout and delivers each to
// its waiter. json.Decoder spans the newline framing for us. A decode error
// (EOF on a dead server, malformed line) ends the loop and fails the connection.
func (t *stdioTransport) readLoop(stdout io.Reader) {
	dec := json.NewDecoder(stdout)
	for {
		var resp rpcResponse
		if err := dec.Decode(&resp); err != nil {
			t.fail()
			return
		}
		if resp.ID == 0 {
			continue // a server-initiated notification: ignore (tools-only scope)
		}
		t.mu.Lock()
		ch := t.pending[resp.ID]
		delete(t.pending, resp.ID)
		t.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

// fail marks the connection dead and unblocks every pending caller (closing their
// channel signals a transport failure). It is idempotent.
func (t *stdioTransport) fail() {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.closed:
		return
	default:
		close(t.closed)
	}
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	select {
	case <-t.closed:
		t.mu.Unlock()
		return nil, errConnClosed
	default:
	}
	t.nextID++
	id := t.nextID
	ch := make(chan rpcResponse, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	// Encode outside t.mu (under writeMu): a write to a full pipe blocks until the
	// server drains it, and holding t.mu across that would also block readLoop from
	// delivering the very responses that let the server drain — a deadlock.
	if err := t.write(rpcRequest{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: raw}); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("mcp: write request: %w", err)
	}

	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errConnClosed
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	t.mu.Lock()
	select {
	case <-t.closed:
		t.mu.Unlock()
		return errConnClosed
	default:
	}
	t.mu.Unlock()
	return t.write(rpcRequest{JSONRPC: jsonRPCVersion, Method: method, Params: raw})
}

// write serializes encoder access under writeMu, held independently of t.mu so a
// write blocked on a full pipe cannot stall the reader or other gating.
func (t *stdioTransport) write(req rpcRequest) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.enc.Encode(req)
}

func (t *stdioTransport) close() error {
	t.fail()
	_ = t.w.Close()
	if t.onClose != nil {
		return t.onClose()
	}
	return nil
}

// envSlice renders an env map as the "KEY=VALUE" slice exec expects, in any
// order (the child re-keys it).
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
