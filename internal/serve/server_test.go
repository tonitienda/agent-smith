package serve

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBackend / fakeSession stand in for the composition root's loop wiring, so
// the protocol adapter is exercised end to end without a provider.
type fakeBackend struct {
	sessions []SessionInfo
	opened   []string // resumeIDs passed to Open, for assertions
	mu       sync.Mutex
}

func (b *fakeBackend) Open(resumeID string, conn Conn) (Session, error) {
	b.mu.Lock()
	b.opened = append(b.opened, resumeID)
	b.mu.Unlock()
	id := resumeID
	if id == "" {
		id = "new-session"
	}
	return &fakeSession{id: id, conn: conn}, nil
}

func (b *fakeBackend) List() ([]SessionInfo, error) { return b.sessions, nil }

type fakeSession struct {
	id     string
	conn   Conn
	closed bool
}

func (s *fakeSession) ID() string { return s.id }

func (s *fakeSession) Run(ctx context.Context, prompt string) (Result, error) {
	switch prompt {
	case "wait": // blocks until cancelled, for the cancellation test
		<-ctx.Done()
		return Result{SessionID: s.id, StopReason: "canceled"}, ctx.Err()
	default:
		s.conn.Emit(Event{Type: "text_delta", Iteration: 0, Text: "hi"})
		dec, err := s.conn.AskPermission(ctx, PermissionRequest{Tool: "shell", Subject: "ls"})
		if err != nil {
			return Result{}, err
		}
		if dec.Allow {
			s.conn.Emit(Event{Type: "tool_finished", Tool: "shell"})
		}
		return Result{Text: "done", SessionID: s.id, StopReason: "end_turn", Iterations: 1, CostUSD: 0.01}, nil
	}
}

func (s *fakeSession) Close() error { s.closed = true; return nil }

// startTestServer runs a Server on a loopback port and returns its address.
func startTestServer(t *testing.T, b Backend) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(b, WithErrorLog(func(string, ...any) {}))
	done := make(chan struct{})
	go func() { _ = srv.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return ln.Addr().String()
}

func TestServeRunTurnStreamsEventsAndForwardsPermission(t *testing.T) {
	b := &fakeBackend{}
	addr := startTestServer(t, b)
	c := dial(t, addr)
	defer c.Close()

	// session.start returns a session id.
	res := c.call(t, "session.start", map[string]any{})
	var start struct {
		SessionID string `json:"session_id"`
	}
	mustResult(t, res, &start)
	if start.SessionID != "new-session" {
		t.Fatalf("session_id = %q, want new-session", start.SessionID)
	}

	// turn.run streams events, forwards the permission ask (auto-allowed by the
	// client pump), and resolves with the Result.
	events, result := c.runTurn(t, "hello", true)
	if len(events) != 2 || events[0].Type != "text_delta" || events[1].Type != "tool_finished" {
		t.Fatalf("events = %+v, want text_delta then tool_finished", events)
	}
	var got Result
	mustResult(t, result, &got)
	if got.Text != "done" || got.Iterations != 1 || got.SessionID != "new-session" {
		t.Fatalf("result = %+v", got)
	}
}

func TestServePermissionDenyStopsTool(t *testing.T) {
	addr := startTestServer(t, &fakeBackend{})
	c := dial(t, addr)
	defer c.Close()
	c.call(t, "session.start", map[string]any{})

	// The client denies the permission ask, so the tool_finished event never fires.
	events, _ := c.runTurn(t, "hello", false)
	for _, e := range events {
		if e.Type == "tool_finished" {
			t.Fatalf("tool ran despite denial: %+v", events)
		}
	}
}

func TestServeCancelTurn(t *testing.T) {
	addr := startTestServer(t, &fakeBackend{})
	c := dial(t, addr)
	defer c.Close()
	c.call(t, "session.start", map[string]any{})

	id := c.send(t, "turn.run", map[string]any{"prompt": "wait"})
	c.call(t, "turn.cancel", map[string]any{})

	resp := c.await(t, id)
	if resp.Error == nil {
		t.Fatalf("cancelled turn should error, got result %s", resp.Result)
	}
}

func TestServeResumeAndList(t *testing.T) {
	b := &fakeBackend{sessions: []SessionInfo{{ID: "abc", Title: "t", EventCount: 3}}}
	addr := startTestServer(t, b)
	c := dial(t, addr)
	defer c.Close()

	res := c.call(t, "session.start", map[string]any{"resume_id": "abc"})
	var start struct {
		SessionID string `json:"session_id"`
	}
	mustResult(t, res, &start)
	if start.SessionID != "abc" {
		t.Fatalf("resume session_id = %q, want abc", start.SessionID)
	}

	res = c.call(t, "session.list", nil)
	var list struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	mustResult(t, res, &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != "abc" {
		t.Fatalf("sessions = %+v", list.Sessions)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	addr := startTestServer(t, &fakeBackend{})
	c := dial(t, addr)
	defer c.Close()
	res := c.call(t, "does.not.exist", nil)
	if res.Error == nil || res.Error.Code != codeMethodNotFound {
		t.Fatalf("want method-not-found error, got %+v", res)
	}
}

func mustResult(t *testing.T, msg rpcMessage, v any) {
	t.Helper()
	if msg.Error != nil {
		t.Fatalf("rpc error: %+v", msg.Error)
	}
	if err := json.Unmarshal(msg.Result, v); err != nil {
		t.Fatalf("decode result %s: %v", msg.Result, err)
	}
}

// --- minimal in-test WebSocket + JSON-RPC client ---

type testClient struct {
	conn   net.Conn
	br     *bufio.Reader
	nextID int
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := "GET / HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("handshake status %q err %v", status, err)
	}
	for { // drain remaining response headers
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &testClient{conn: conn, br: br}
}

func (c *testClient) Close() { _ = c.conn.Close() }

// send writes a request and returns its id (raw JSON) without waiting.
func (c *testClient) send(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	c.nextID++
	id := json.RawMessage(fmt.Sprintf("%d", c.nextID))
	msg := rpcMessage{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		msg.Params = mustRaw(params)
	}
	b, _ := json.Marshal(msg)
	if err := c.writeFrame(b); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	return id
}

// call sends a request and returns its response, ignoring any notifications.
func (c *testClient) call(t *testing.T, method string, params any) rpcMessage {
	t.Helper()
	id := c.send(t, method, params)
	return c.await(t, id)
}

// await reads until the response for id arrives, ignoring events/other traffic.
func (c *testClient) await(t *testing.T, id json.RawMessage) rpcMessage {
	t.Helper()
	for {
		msg := c.read(t)
		if msg.Method == "" && string(msg.ID) == string(id) {
			return msg
		}
	}
}

// runTurn sends turn.run, answering any permission.ask with allow, collecting
// events until the turn's response arrives.
func (c *testClient) runTurn(t *testing.T, prompt string, allow bool) ([]Event, rpcMessage) {
	t.Helper()
	id := c.send(t, "turn.run", map[string]any{"prompt": prompt})
	var events []Event
	for {
		msg := c.read(t)
		switch {
		case msg.Method == "event":
			var ev Event
			_ = json.Unmarshal(msg.Params, &ev)
			events = append(events, ev)
		case msg.Method == "permission.ask":
			resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: mustRaw(PermissionDecision{Allow: allow})}
			b, _ := json.Marshal(resp)
			if err := c.writeFrame(b); err != nil {
				t.Fatalf("write permission reply: %v", err)
			}
		case msg.Method == "" && string(msg.ID) == string(id):
			return events, msg
		}
	}
}

func (c *testClient) read(t *testing.T) rpcMessage {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	payload, err := c.readFrame()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var msg rpcMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	return msg
}

// writeFrame writes a masked single text frame (clients must mask, RFC 6455).
func (c *testClient) writeFrame(payload []byte) error {
	n := len(payload)
	var header []byte
	b0 := byte(0x81) // FIN | text
	switch {
	case n < 126:
		header = []byte{b0, 0x80 | byte(n)}
	case n < 1<<16:
		header = []byte{b0, 0x80 | 126, byte(n >> 8), byte(n)}
	default:
		header = make([]byte, 4+8)
		header[0], header[1] = b0, 0x80|127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i&3]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if _, err := c.conn.Write(mask[:]); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

// readFrame reads one unmasked server data frame (no fragmentation in practice).
func (c *testClient) readFrame() ([]byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(c.br, head[:]); err != nil {
		return nil, err
	}
	opcode := head[0] & 0x0f
	if opcode == opClose {
		return nil, errors.New("server closed")
	}
	n := int64(head[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return nil, err
		}
		n = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return nil, err
		}
		n = int64(binary.BigEndian.Uint64(ext[:]))
	}
	payload := make([]byte, n)
	_, err := io.ReadFull(c.br, payload)
	return payload, err
}
