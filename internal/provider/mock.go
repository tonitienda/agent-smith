package provider

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/tonitienda/agent-smith/schema"
)

// Mock is an in-memory Provider for tests (AS-008): the loop tests (AS-018) and
// the conformance suite (AS-012) drive the core against it without importing a
// real provider, satisfying the "core depends only on the interface" rule.
//
// A test scripts a turn either statically (Events plus an optional terminating
// StreamErr) or dynamically (ScriptFn, which sees the Request and returns the
// events and terminating error). Every Request is recorded in Requests for
// assertions. The zero value is a usable provider that yields no events.
type Mock struct {
	// NameValue overrides Name; defaults to "mock".
	NameValue string

	// OpenErr, when set, makes Stream fail immediately (before any event),
	// modeling a request that could not be started.
	OpenErr error

	// Events is the static script returned for every request when ScriptFn is
	// nil. StreamErr, when set, is the terminating error reported by the stream's
	// Err after those events are delivered (a mid/terminal stream failure).
	Events    []Event
	StreamErr error

	// ScriptFn, when set, supersedes Events/StreamErr: it sees the request and
	// returns that turn's events and terminating error (nil for a clean end).
	ScriptFn func(ctx context.Context, req Request) ([]Event, error)

	mu       sync.Mutex
	requests []Request
}

// compile-time check that *Mock satisfies the interface the core depends on.
var _ Provider = (*Mock)(nil)

// Name reports NameValue, or "mock" when unset.
func (m *Mock) Name() string {
	if m.NameValue != "" {
		return m.NameValue
	}
	return "mock"
}

// Stream records req, then returns a stream replaying the scripted events. It
// honors ctx cancellation and OpenErr before producing a stream.
func (m *Mock) Stream(ctx context.Context, req Request) (Stream, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.OpenErr != nil {
		return nil, m.OpenErr
	}

	events, streamErr := m.Events, m.StreamErr
	if m.ScriptFn != nil {
		events, streamErr = m.ScriptFn(ctx, req)
	}
	return &sliceStream{events: events, err: streamErr}, nil
}

// Requests returns a copy of the requests Stream has received, in order.
func (m *Mock) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.requests...)
}

// sliceStream replays a fixed slice of events and then reports err. It backs the
// Mock and the *Turn builders, and is the reference Stream implementation.
type sliceStream struct {
	events []Event
	idx    int
	cur    Event
	err    error
	closed bool
}

func (s *sliceStream) Next() bool {
	if s.closed || s.idx >= len(s.events) {
		return false
	}
	s.cur = s.events[s.idx]
	s.idx++
	return true
}

func (s *sliceStream) Event() Event { return s.cur }

func (s *sliceStream) Err() error { return s.err }

func (s *sliceStream) Close() error {
	s.closed = true
	return nil
}

// SliceStream returns a Stream that replays events and then reports err from
// Err. It lets tests and adapters build a Stream from a precomputed event slice
// (e.g. a non-streaming provider that assembles a whole turn, then re-streams
// it). A nil err means a clean end.
func SliceStream(events []Event, err error) Stream {
	return &sliceStream{events: events, err: err}
}

// TextTurn scripts a single text-only assistant turn: a turn start, one text
// block carrying text, and a turn stop. An empty stopReason defaults to
// StopEndTurn.
func TextTurn(text, stopReason string) []Event {
	if stopReason == "" {
		stopReason = StopEndTurn
	}
	return []Event{
		{Type: EventTurnStart, Turn: &TurnInfo{}},
		{Type: EventBlockStart, Header: &BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}},
		{Type: EventTextDelta, TextDelta: text},
		{Type: EventBlockStop},
		{Type: EventTurnStop, StopReason: stopReason},
	}
}

// ToolCallTurn scripts a single assistant turn that calls one client tool with
// the given id, name, and JSON arguments, stopping with StopToolUse. It is the
// common fixture for loop tests (AS-018) that exercise a tool round-trip.
func ToolCallTurn(toolUseID, name string, args json.RawMessage) []Event {
	return []Event{
		{Type: EventTurnStart, Turn: &TurnInfo{}},
		{Type: EventBlockStart, Header: &BlockHeader{
			Kind:      schema.KindToolCall,
			Role:      schema.RoleAssistant,
			ToolUseID: toolUseID,
			ToolName:  name,
			ToolKind:  schema.ToolKindClient,
		}},
		{Type: EventToolCallDelta, ArgumentsDelta: string(args)},
		{Type: EventBlockStop},
		{Type: EventTurnStop, StopReason: StopToolUse},
	}
}
