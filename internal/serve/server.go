// Package serve is Agent Smith's local JSON-RPC/WebSocket session face (AS-077):
// the single-user programmatic spine every graphical face (the web GUI AS-078,
// the Viscose extension AS-081) drives. It exposes the existing face-agnostic
// core — start/resume a session, run a turn, cancel it, list sessions — over
// JSON-RPC 2.0 framed on a WebSocket, reusing the headless plumbing (AS-051)
// rather than re-implementing the loop.
//
// The package is a pure protocol adapter: it owns the transport (a minimal,
// stdlib-only RFC 6455 codec, see websocket.go) and the JSON-RPC dispatch, and
// holds no business logic (AC5). The composition root (cmd/smith) implements the
// Backend — building the loop, tools, permission gate, and hooks — so this face
// stays stdlib-first like the rest of the core (PRD D6) and the loop never learns
// about it. Server→client notifications carry the loop's UIEvent stream; an
// ask-mode permission prompt (AS-016) is forwarded to the client as a
// server-initiated request, and a client that cannot answer fails fast to a
// denial rather than hanging (D-CLI-9).
//
// Binding policy (loopback by default; the AS-080 sandboxing caveat) lives in the
// composition root, which hands this server a ready net.Listener.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// Event is a face-agnostic turn event forwarded to the client as a JSON-RPC
// notification (method "event"). It mirrors the loop's UIEvent flattened to the
// fields a client can render; the composition root maps loop.UIEvent onto it so
// this package stays free of the loop. New fields are additive (PRD D2).
type Event struct {
	Type       string  `json:"type"`
	Iteration  int     `json:"iteration"`
	Text       string  `json:"text,omitempty"`
	Tool       string  `json:"tool,omitempty"`
	StopReason string  `json:"stop_reason,omitempty"`
	SpentUSD   float64 `json:"spent_usd,omitempty"`
	LimitUSD   float64 `json:"limit_usd,omitempty"`
}

// Result is the structured outcome of a completed turn (the "turn.run" reply):
// the assistant's final text, the session it belongs to (so it is resumable), and
// what the turn cost and why it stopped — the same shape as the headless
// runResult (AS-051), minus the headless-only denial report.
type Result struct {
	Text       string  `json:"text"`
	SessionID  string  `json:"session_id"`
	StopReason string  `json:"stop_reason"`
	CostUSD    float64 `json:"cost_usd"`
	Iterations int     `json:"iterations"`
}

// SessionInfo is one entry of the "session.list" reply.
type SessionInfo struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	EventCount int    `json:"event_count,omitempty"`
}

// PermissionRequest is forwarded to the client as the params of a server-initiated
// "permission.ask" request when a tool call needs interactive approval (AS-016).
type PermissionRequest struct {
	Tool      string          `json:"tool"`
	Subject   string          `json:"subject,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// PermissionDecision is the client's answer to a PermissionRequest.
type PermissionDecision struct {
	Allow bool `json:"allow"`
}

// Conn is the client handle the Backend wires its session against: it streams
// turn events to the client and forwards permission prompts. The composition
// root's permission Asker calls AskPermission; the loop observer calls Emit.
type Conn interface {
	// Emit sends a turn event to the client (a JSON-RPC notification).
	Emit(Event)
	// AskPermission forwards a permission prompt to the client and blocks for the
	// answer or ctx cancellation. An error is treated by the caller as a denial,
	// so a client that cannot render prompts fails fast rather than hanging.
	AskPermission(ctx context.Context, req PermissionRequest) (PermissionDecision, error)
}

// Session is one connection's live session driver, built by the Backend. A turn
// runs against the same session/engine across calls (multi-turn parity with the
// TUI); events and permission prompts flow through the Conn captured at Open.
type Session interface {
	// ID is the stable session identifier (resumable, AS-051 AC4).
	ID() string
	// Run drives a single user turn, returning its Result. ctx cancellation stops
	// the turn (turn.cancel over the wire), with the loop reconciling in-flight
	// tool calls.
	Run(ctx context.Context, prompt string) (Result, error)
	// Close releases the session (fires session-stop, closes the log).
	Close() error
}

// Backend is what the serve face needs from the composition root: it builds a
// per-connection Session (fresh when resumeID is empty, else resumed) wired to
// the given Conn, and lists the project's sessions. Keeping the loop wiring here
// is what makes serve a logic-free protocol adapter (AC5).
type Backend interface {
	Open(resumeID string, conn Conn) (Session, error)
	List() ([]SessionInfo, error)
}

// Server serves the JSON-RPC session protocol over WebSocket on a net.Listener.
type Server struct {
	backend Backend
	logf    func(string, ...any)

	mu    sync.Mutex
	conns map[*wsConn]struct{}
}

// Option configures a Server.
type Option func(*Server)

// WithErrorLog sets a sink for non-fatal connection diagnostics (a malformed
// frame, a dropped client). Without one, such errors are swallowed: a single
// client's misbehavior must not take the server down.
func WithErrorLog(logf func(string, ...any)) Option {
	return func(s *Server) { s.logf = logf }
}

// NewServer builds a Server over backend.
func NewServer(backend Backend, opts ...Option) *Server {
	s := &Server{backend: backend, logf: func(string, ...any) {}, conns: map[*wsConn]struct{}{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Serve accepts WebSocket connections on ln until ctx is cancelled, then stops
// accepting and closes any open connections. It returns nil on a clean shutdown.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	hs := &http.Server{Handler: http.HandlerFunc(s.handle)}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		// Close the HTTP server (stops accepting) and any hijacked WebSocket
		// connections, which Server.Close does not own once hijacked.
		_ = hs.Close()
		s.closeConns()
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// handle upgrades an HTTP request to WebSocket and runs the per-connection loop.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.track(ws)
	defer s.untrack(ws)
	c := &conn{ws: ws, backend: s.backend, logf: s.logf, pending: map[string]chan rpcMessage{}}
	c.serve()
}

func (s *Server) track(ws *wsConn) {
	s.mu.Lock()
	s.conns[ws] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrack(ws *wsConn) {
	s.mu.Lock()
	delete(s.conns, ws)
	s.mu.Unlock()
}

func (s *Server) closeConns() {
	s.mu.Lock()
	conns := make([]*wsConn, 0, len(s.conns))
	for ws := range s.conns {
		conns = append(conns, ws)
	}
	s.mu.Unlock()
	for _, ws := range conns {
		_ = ws.Close()
	}
}

// rpcMessage is one JSON-RPC 2.0 frame in either direction. ID is kept raw so a
// client's id (number or string) round-trips exactly on the reply, while the
// server uses its own numeric ids for server-initiated requests. A message with
// a Method is a request (id set) or notification (id absent); one without is a
// response to a server-initiated request.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes: the standard ones plus the serve-specific application
// range (-32000..-32099, RFC-reserved for server use).
const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeRunFailed      = -32000
	codeNoSession      = -32002
	codeTurnBusy       = -32003
)

// conn drives one WebSocket connection: it reads JSON-RPC frames, dispatches
// client requests, streams events, and correlates the responses to its own
// server-initiated permission requests. It implements Conn.
type conn struct {
	ws      *wsConn
	backend Backend
	logf    func(string, ...any)

	mu      sync.Mutex
	sess    Session
	pending map[string]chan rpcMessage
	nextID  int

	turnMu sync.Mutex
	cancel context.CancelFunc
}

// serve runs the read loop until the connection closes.
func (c *conn) serve() {
	defer func() { _ = c.ws.Close() }()
	defer func() {
		// Cancel any in-flight turn so a client disconnect does not leave the turn
		// goroutine running (and burning provider/tool work) after the read loop ends.
		c.turnMu.Lock()
		cancel := c.cancel
		c.turnMu.Unlock()
		if cancel != nil {
			cancel()
		}
		c.mu.Lock()
		sess := c.sess
		c.mu.Unlock()
		if sess != nil {
			_ = sess.Close()
		}
	}()
	for {
		data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.reply(nil, nil, &rpcError{Code: codeParse, Message: "parse error: " + err.Error()})
			continue
		}
		c.handle(msg)
	}
}

// handle routes one decoded frame. A frame with no method is a response to a
// server-initiated request; otherwise it is a client request to dispatch.
func (c *conn) handle(msg rpcMessage) {
	if msg.Method == "" {
		if len(msg.ID) == 0 {
			return
		}
		c.mu.Lock()
		ch := c.pending[string(msg.ID)]
		delete(c.pending, string(msg.ID))
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
		return
	}
	switch msg.Method {
	case "session.start":
		c.handleStart(msg)
	case "session.list":
		c.handleList(msg)
	case "turn.run":
		c.handleRun(msg)
	case "turn.cancel":
		c.handleCancel(msg)
	default:
		c.reply(msg.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + msg.Method})
	}
}

func (c *conn) handleStart(msg rpcMessage) {
	var p struct {
		ResumeID string `json:"resume_id"`
	}
	if len(msg.Params) > 0 {
		_ = json.Unmarshal(msg.Params, &p)
	}
	sess, err := c.backend.Open(p.ResumeID, c)
	if err != nil {
		c.reply(msg.ID, nil, &rpcError{Code: codeRunFailed, Message: err.Error()})
		return
	}
	// Cancel any turn still running against the previous session before swapping it
	// out, so its goroutine cannot keep streaming stale events to the client.
	c.turnMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.turnMu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.mu.Lock()
	old := c.sess
	c.sess = sess
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	c.reply(msg.ID, map[string]string{"session_id": sess.ID()}, nil)
}

func (c *conn) handleList(msg rpcMessage) {
	list, err := c.backend.List()
	if err != nil {
		c.reply(msg.ID, nil, &rpcError{Code: codeRunFailed, Message: err.Error()})
		return
	}
	if list == nil {
		list = []SessionInfo{}
	}
	c.reply(msg.ID, map[string]any{"sessions": list}, nil)
}

// handleRun runs on the read loop: it validates the session, registers the
// turn's cancel synchronously (so a turn.cancel that follows cannot race ahead of
// the registration), then drives the turn itself off the read loop so cancel and
// the permission response stay processable while the turn is in flight.
func (c *conn) handleRun(msg rpcMessage) {
	c.mu.Lock()
	sess := c.sess
	c.mu.Unlock()
	if sess == nil {
		c.reply(msg.ID, nil, &rpcError{Code: codeNoSession, Message: "no session: call session.start first"})
		return
	}
	var p struct {
		Prompt string `json:"prompt"`
	}
	if len(msg.Params) > 0 {
		_ = json.Unmarshal(msg.Params, &p)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.turnMu.Lock()
	if c.cancel != nil {
		c.turnMu.Unlock()
		cancel()
		c.reply(msg.ID, nil, &rpcError{Code: codeTurnBusy, Message: "a turn is already running"})
		return
	}
	c.cancel = cancel
	c.turnMu.Unlock()

	go func() {
		res, err := sess.Run(ctx, p.Prompt)
		c.turnMu.Lock()
		c.cancel = nil
		c.turnMu.Unlock()
		cancel()
		if err != nil {
			c.reply(msg.ID, nil, &rpcError{Code: codeRunFailed, Message: err.Error()})
			return
		}
		c.reply(msg.ID, res, nil)
	}()
}

func (c *conn) handleCancel(msg rpcMessage) {
	c.turnMu.Lock()
	cancel := c.cancel
	c.turnMu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.reply(msg.ID, map[string]string{}, nil)
}

// Emit streams a turn event to the client as a notification.
func (c *conn) Emit(ev Event) {
	_ = c.send(rpcMessage{Method: "event", Params: mustRaw(ev)})
}

// AskPermission sends a server-initiated permission request and blocks for the
// client's response or ctx cancellation. A transport error or cancellation is
// returned to the caller, which treats it as a denial (fail fast, never hang).
func (c *conn) AskPermission(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
	c.mu.Lock()
	c.nextID++
	id := json.RawMessage(strconv.Itoa(c.nextID))
	ch := make(chan rpcMessage, 1)
	c.pending[string(id)] = ch
	c.mu.Unlock()

	if err := c.send(rpcMessage{ID: id, Method: "permission.ask", Params: mustRaw(req)}); err != nil {
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
		return PermissionDecision{}, err
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
		return PermissionDecision{}, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return PermissionDecision{}, errors.New(resp.Error.Message)
		}
		var dec PermissionDecision
		if len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, &dec); err != nil {
				return PermissionDecision{}, err
			}
		}
		return dec, nil
	}
}

// reply sends a response to a client request: result on success, or an error.
func (c *conn) reply(id json.RawMessage, result any, rerr *rpcError) {
	msg := rpcMessage{ID: id, Error: rerr}
	if rerr == nil {
		if result == nil {
			result = struct{}{}
		}
		msg.Result = mustRaw(result)
	}
	_ = c.send(msg)
}

// send marshals and writes one frame, logging (not failing) on a write error.
func (c *conn) send(msg rpcMessage) error {
	msg.JSONRPC = "2.0"
	b, err := json.Marshal(msg)
	if err != nil {
		c.logf("serve: marshal message: %v", err)
		return err
	}
	if err := c.ws.WriteMessage(b); err != nil {
		c.logf("serve: write message: %v", err)
		return err
	}
	return nil
}

// mustRaw marshals v to a json.RawMessage, falling back to null on the
// (unexpected) marshal error so a single bad payload cannot panic the server.
func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
