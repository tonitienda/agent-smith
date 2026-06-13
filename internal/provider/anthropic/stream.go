package anthropic

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// sseStream reads the Anthropic Messages SSE response and normalizes each event
// into the provider package's uniform Event stream. It follows the Scanner idiom
// (Next/Event/Err/Close). A single Anthropic event can yield several normalized
// events (message_start → turn start + input usage; message_delta → output usage
// + a remembered stop reason), so translate enqueues into pending and Next pops
// one at a time — nothing is buffered to the end of the turn, so deltas surface
// incrementally.
type sseStream struct {
	body io.ReadCloser
	br   *bufio.Reader

	pending []provider.Event // normalized events not yet returned by Next
	cur     provider.Event
	stop    string // stop reason captured from message_delta, emitted at message_stop
	err     error  // terminating error (a *provider.Error when provider-shaped)
	done    bool   // the underlying stream reached a clean end
	closed  bool
}

func newSSEStream(body io.ReadCloser) *sseStream {
	return &sseStream{body: body, br: bufio.NewReader(body)}
}

func (s *sseStream) Next() bool {
	if s.closed {
		return false
	}
	for len(s.pending) == 0 {
		if s.err != nil || s.done {
			return false
		}
		data, err := s.readSSEEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.done = true
				return false
			}
			e := provider.New(provider.ErrUnknown, "reading stream: %v", err)
			e.Retryable = true
			e.Err = err
			s.err = e
			return false
		}
		// translate may set s.err (an Anthropic error frame); the loop re-checks
		// s.err at the top before reading again.
		s.pending = s.translate(data)
	}
	s.cur = s.pending[0]
	s.pending = s.pending[1:]
	return true
}

func (s *sseStream) Event() provider.Event { return s.cur }

func (s *sseStream) Err() error { return s.err }

func (s *sseStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

// readSSEEvent reads one Server-Sent Event, returning its concatenated data
// payload. SSE frames are separated by a blank line; data may span multiple
// `data:` lines (joined with "\n"); `event:`, `id:`, and comment lines are
// ignored because the payload's own "type" field discriminates the event. It
// returns io.EOF only at a clean end with no buffered data.
func (s *sseStream) readSSEEvent() ([]byte, error) {
	var data []byte
	haveData := false
	for {
		line, err := s.br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if haveData {
					return data, nil
				}
				// Blank line before any data (e.g. between comment frames): keep reading.
			case strings.HasPrefix(trimmed, "data:"):
				v := strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " ")
				if haveData {
					data = append(data, '\n')
				}
				data = append(data, v...)
				haveData = true
			default:
				// event:/id:/comment line — ignored.
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && haveData {
				return data, nil
			}
			return nil, err
		}
	}
}

// sseFrame is the union of the Anthropic stream event shapes the adapter reads.
// Only the fields relevant to a given event type are populated; "type"
// discriminates.
type sseFrame struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID    string     `json:"id"`
		Model string     `json:"model"`
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
		Data      string `json:"data"`
		ID        string `json:"id"`
		Name      string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// wireUsage is the Anthropic usage object as it appears on message_start and
// message_delta. Counts are pointers so an unreported field stays nil (never a
// misleading zero), matching schema.Tokens.
type wireUsage struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
	CacheCreation            *struct {
		Ephemeral5m *int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h *int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServiceTier   string          `json:"service_tier"`
	ServerToolUse json.RawMessage `json:"server_tool_use"`
}

// translate normalizes one Anthropic SSE frame into zero or more provider
// Events. A malformed frame or an error frame terminates the stream via s.err.
func (s *sseStream) translate(data []byte) []provider.Event {
	var f sseFrame
	if err := json.Unmarshal(data, &f); err != nil {
		e := provider.New(provider.ErrUnknown, "decoding stream event: %v", err)
		e.Err = err
		s.err = e
		return nil
	}

	switch f.Type {
	case "message_start":
		return s.onMessageStart(&f)
	case "content_block_start":
		return s.onContentBlockStart(&f)
	case "content_block_delta":
		return s.onContentBlockDelta(&f)
	case "content_block_stop":
		return []provider.Event{{Type: provider.EventBlockStop, BlockIndex: f.Index}}
	case "message_delta":
		return s.onMessageDelta(&f)
	case "message_stop":
		stop := s.stop
		if stop == "" {
			stop = provider.StopEndTurn
		}
		return []provider.Event{{Type: provider.EventTurnStop, StopReason: stop}}
	case "error":
		s.err = mapStreamError(f.Error)
		return nil
	case "ping":
		return nil
	default:
		// Unknown event type — additive tolerance (PRD D2): ignore it.
		return nil
	}
}

func (s *sseStream) onMessageStart(f *sseFrame) []provider.Event {
	if f.Message == nil {
		return nil
	}
	out := []provider.Event{{
		Type: provider.EventTurnStart,
		Turn: &provider.TurnInfo{ResponseID: f.Message.ID, Model: f.Message.Model},
	}}
	// Input and cache counts arrive at the start; the output count arrives at
	// message_delta. Reporting each only where Anthropic reports it lets a
	// consumer that accumulates usage avoid double-counting (the placeholder
	// output_tokens on message_start is intentionally dropped).
	if tok := startUsage(f.Message.Usage); tok != nil {
		ev := provider.Event{Type: provider.EventUsage, Usage: tok}
		if meta := usageMeta(f.Message.Usage); meta != nil {
			ev.UsageMeta = meta
		}
		out = append(out, ev)
	}
	return out
}

func (s *sseStream) onContentBlockStart(f *sseFrame) []provider.Event {
	cb := f.ContentBlock
	if cb == nil {
		return nil
	}
	header := &provider.BlockHeader{Role: schema.RoleAssistant}
	switch cb.Type {
	case "thinking", "redacted_thinking":
		header.Kind = schema.KindReasoning
	case "tool_use":
		header.Kind = schema.KindToolCall
		header.ToolUseID = cb.ID
		header.ToolName = cb.Name
		header.ToolKind = schema.ToolKindClient
	case "server_tool_use":
		header.Kind = schema.KindToolCall
		header.ToolUseID = cb.ID
		header.ToolName = cb.Name
		header.ToolKind = schema.ToolKindServer
		header.ToolSubtype = cb.Name
	default:
		// "text" and any unmodeled block type assemble as text.
		header.Kind = schema.KindText
	}

	out := []provider.Event{{Type: provider.EventBlockStart, BlockIndex: f.Index, Header: header}}

	// Redacted thinking carries its opaque payload on the start frame (no
	// deltas); forward it verbatim so the assembled block keeps it.
	if cb.Type == "redacted_thinking" && cb.Data != "" {
		out = append(out, provider.Event{Type: provider.EventReasoningDelta, BlockIndex: f.Index, EncryptedDelta: cb.Data})
	}
	return out
}

func (s *sseStream) onContentBlockDelta(f *sseFrame) []provider.Event {
	if f.Delta == nil {
		return nil
	}
	switch f.Delta.Type {
	case "text_delta":
		return []provider.Event{{Type: provider.EventTextDelta, BlockIndex: f.Index, TextDelta: f.Delta.Text}}
	case "thinking_delta":
		return []provider.Event{{Type: provider.EventReasoningDelta, BlockIndex: f.Index, TextDelta: f.Delta.Thinking}}
	case "signature_delta":
		return []provider.Event{{Type: provider.EventReasoningDelta, BlockIndex: f.Index, SignatureDelta: f.Delta.Signature}}
	case "input_json_delta":
		return []provider.Event{{Type: provider.EventToolCallDelta, BlockIndex: f.Index, ArgumentsDelta: f.Delta.PartialJSON}}
	default:
		return nil
	}
}

func (s *sseStream) onMessageDelta(f *sseFrame) []provider.Event {
	var out []provider.Event
	if tok := deltaUsage(f.Usage); tok != nil {
		out = append(out, provider.Event{Type: provider.EventUsage, Usage: tok})
	}
	if f.Delta != nil && f.Delta.StopReason != "" {
		s.stop = mapStopReason(f.Delta.StopReason)
	}
	return out
}

// mapStopReason maps an Anthropic stop_reason onto the normalized Stop* set.
func mapStopReason(r string) string {
	switch r {
	case "end_turn", "stop_sequence":
		return provider.StopEndTurn
	case "max_tokens":
		return provider.StopMaxTokens
	case "tool_use":
		return provider.StopToolUse
	case "pause_turn":
		return provider.StopPauseTurn
	case "refusal":
		return provider.StopRefusal
	case "model_context_window_exceeded":
		return provider.StopContextWindowExceeded
	default:
		return r
	}
}

// startUsage builds the input/cache portion of usage from a message_start usage
// object, deliberately omitting the placeholder output count. Returns nil when
// nothing is reported.
func startUsage(u *wireUsage) *schema.Tokens {
	if u == nil {
		return nil
	}
	tok := &schema.Tokens{
		Input:      copyInt(u.InputTokens),
		CacheRead:  copyInt(u.CacheReadInputTokens),
		CacheWrite: copyInt(u.CacheCreationInputTokens),
	}
	if u.CacheCreation != nil {
		tok.CacheWrite5m = copyInt(u.CacheCreation.Ephemeral5m)
		tok.CacheWrite1h = copyInt(u.CacheCreation.Ephemeral1h)
	}
	if isEmptyTokens(tok) {
		return nil
	}
	return tok
}

// deltaUsage builds the output portion of usage from a message_delta usage
// object. Returns nil when no output count is reported.
func deltaUsage(u *wireUsage) *schema.Tokens {
	if u == nil || u.OutputTokens == nil {
		return nil
	}
	return &schema.Tokens{Output: copyInt(u.OutputTokens)}
}

// usageMeta captures price-affecting metadata (service tier, server-tool counts)
// when present, or nil.
func usageMeta(u *wireUsage) *schema.UsageMeta {
	if u == nil || (u.ServiceTier == "" && len(u.ServerToolUse) == 0) {
		return nil
	}
	return &schema.UsageMeta{ServiceTier: u.ServiceTier, ServerToolUse: u.ServerToolUse}
}

func isEmptyTokens(t *schema.Tokens) bool {
	return t.Input == nil && t.CacheRead == nil && t.CacheWrite == nil &&
		t.CacheWrite5m == nil && t.CacheWrite1h == nil
}

// copyInt returns a pointer to a copy of *p, or nil when p is nil, so the
// normalized event never aliases the decoded frame.
func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
