package openai

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// responsesStream reads the OpenAI Responses SSE response and normalizes each
// event into the provider package's uniform Event stream. It follows the Scanner
// idiom (Next/Event/Err/Close). A single Responses event can yield several
// normalized events (response.completed → usage + turn stop), so translate
// enqueues into pending and Next pops one at a time — nothing is buffered to the
// end of the turn, so deltas surface incrementally.
type responsesStream struct {
	r sseReader

	pending []provider.Event
	cur     provider.Event

	sawToolCall bool // a function_call item was produced this turn
	sawRefusal  bool // a refusal was streamed this turn

	err    error
	done   bool
	closed bool
}

func newResponsesStream(body io.ReadCloser) *responsesStream {
	return &responsesStream{r: newSSEReader(body)}
}

func (s *responsesStream) Next() bool {
	if s.closed {
		return false
	}
	for len(s.pending) == 0 {
		if s.err != nil || s.done {
			return false
		}
		data, err := s.r.readEvent()
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
		s.pending = s.translate(data)
	}
	s.cur = s.pending[0]
	s.pending = s.pending[1:]
	return true
}

func (s *responsesStream) Event() provider.Event { return s.cur }

func (s *responsesStream) Err() error { return s.err }

func (s *responsesStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.r.body.Close()
}

// responsesFrame is the union of the Responses stream event shapes the adapter
// reads. Only the fields relevant to a given event type are populated; "type"
// discriminates.
type responsesFrame struct {
	Type        string             `json:"type"`
	OutputIndex int                `json:"output_index"`
	Delta       string             `json:"delta"`
	Item        *responsesItem     `json:"item"`
	Response    *responsesResponse `json:"response"`
	// Top-level error event fields (type == "error").
	Message string `json:"message"`
	Code    string `json:"code"`
}

type responsesItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	EncryptedContent string `json:"encrypted_content"`
}

type responsesResponse struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	Status            string `json:"status"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *responsesUsage `json:"usage"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// responsesUsage is the Responses usage object. Counts are pointers so an
// unreported field stays nil (never a misleading zero), matching schema.Tokens.
type responsesUsage struct {
	InputTokens        *int `json:"input_tokens"`
	OutputTokens       *int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

// translate normalizes one Responses SSE frame into zero or more provider
// Events. A malformed frame or an error/failed frame terminates the stream.
func (s *responsesStream) translate(data []byte) []provider.Event {
	var f responsesFrame
	if err := json.Unmarshal(data, &f); err != nil {
		e := provider.New(provider.ErrUnknown, "decoding stream event: %v", err)
		e.Err = err
		s.err = e
		return nil
	}

	switch f.Type {
	case "response.created":
		return s.onCreated(&f)
	case "response.output_item.added":
		return s.onItemAdded(&f)
	case "response.output_text.delta":
		return []provider.Event{{Type: provider.EventTextDelta, BlockIndex: f.OutputIndex, TextDelta: f.Delta}}
	case "response.refusal.delta":
		s.sawRefusal = true
		return []provider.Event{{Type: provider.EventTextDelta, BlockIndex: f.OutputIndex, TextDelta: f.Delta}}
	case "response.function_call_arguments.delta":
		return []provider.Event{{Type: provider.EventToolCallDelta, BlockIndex: f.OutputIndex, ArgumentsDelta: f.Delta}}
	case "response.reasoning_summary_text.delta":
		return []provider.Event{{Type: provider.EventReasoningDelta, BlockIndex: f.OutputIndex, TextDelta: f.Delta}}
	case "response.output_item.done":
		return s.onItemDone(&f)
	case "response.completed", "response.incomplete":
		return s.onTerminal(&f)
	case "response.failed":
		s.err = mapResponsesFailure(&f)
		return nil
	case "error":
		s.err = mapResponsesErrorEvent(&f)
		return nil
	default:
		// Unknown/uninteresting event (response.in_progress, content_part.added,
		// *.done deltas, …) — additive tolerance (PRD D2): ignore it.
		return nil
	}
}

func (s *responsesStream) onCreated(f *responsesFrame) []provider.Event {
	if f.Response == nil {
		return nil
	}
	return []provider.Event{{
		Type: provider.EventTurnStart,
		Turn: &provider.TurnInfo{ResponseID: f.Response.ID, Model: f.Response.Model},
	}}
}

func (s *responsesStream) onItemAdded(f *responsesFrame) []provider.Event {
	it := f.Item
	if it == nil {
		return nil
	}
	header := &provider.BlockHeader{Role: schema.RoleAssistant}
	switch it.Type {
	case "reasoning":
		header.Kind = schema.KindReasoning
	case "function_call":
		s.sawToolCall = true
		header.Kind = schema.KindToolCall
		header.ToolUseID = it.CallID
		header.ToolName = it.Name
		header.ToolKind = schema.ToolKindClient
	case "message":
		header.Kind = schema.KindText
	default:
		// Server-side tool calls (web_search_call, file_search_call,
		// code_interpreter_call, mcp_call, …): the provider runs them, so they
		// open as server tool_call blocks with the item type as the subtype.
		if isServerToolCall(it.Type) {
			s.sawToolCall = true
			header.Kind = schema.KindToolCall
			header.ToolUseID = firstNonEmpty(it.CallID, it.ID)
			header.ToolName = serverToolName(it.Type)
			header.ToolKind = schema.ToolKindServer
			header.ToolSubtype = it.Type
		} else {
			// Any other unmodeled item assembles as text.
			header.Kind = schema.KindText
		}
	}
	return []provider.Event{{Type: provider.EventBlockStart, BlockIndex: f.OutputIndex, Header: header}}
}

func (s *responsesStream) onItemDone(f *responsesFrame) []provider.Event {
	var out []provider.Event
	// A reasoning item carries its opaque encrypted_content on the done frame
	// (for stateless reuse); forward it verbatim so the assembled block keeps it.
	if f.Item != nil && f.Item.Type == "reasoning" && f.Item.EncryptedContent != "" {
		out = append(out, provider.Event{
			Type:           provider.EventReasoningDelta,
			BlockIndex:     f.OutputIndex,
			EncryptedDelta: f.Item.EncryptedContent,
		})
	}
	out = append(out, provider.Event{Type: provider.EventBlockStop, BlockIndex: f.OutputIndex})
	return out
}

func (s *responsesStream) onTerminal(f *responsesFrame) []provider.Event {
	var out []provider.Event
	if f.Response != nil {
		if tok := responsesTokens(f.Response.Usage); tok != nil {
			out = append(out, provider.Event{Type: provider.EventUsage, Usage: tok})
		}
	}
	out = append(out, provider.Event{Type: provider.EventTurnStop, StopReason: s.stopReason(f)})
	return out
}

// stopReason derives the normalized stop reason. The Responses API has no single
// finish_reason field, so it is inferred from the response status and the items
// seen: an incomplete response reports its reason (max_output_tokens), a turn
// that produced a function_call is a tool-use turn, a refusal is a refusal turn,
// and everything else is a natural end.
func (s *responsesStream) stopReason(f *responsesFrame) string {
	if f != nil && f.Response != nil {
		r := f.Response
		if r.Status == "incomplete" && r.IncompleteDetails != nil {
			switch r.IncompleteDetails.Reason {
			case "max_output_tokens":
				return provider.StopMaxTokens
			case "content_filter":
				return provider.StopContentFilter
			}
		}
	}
	switch {
	case s.sawRefusal:
		return provider.StopRefusal
	case s.sawToolCall:
		return provider.StopToolUse
	default:
		return provider.StopEndTurn
	}
}

// responsesTokens maps a Responses usage object onto schema.Tokens, or nil when
// nothing is reported.
func responsesTokens(u *responsesUsage) *schema.Tokens {
	if u == nil {
		return nil
	}
	tok := &schema.Tokens{Input: copyInt(u.InputTokens), Output: copyInt(u.OutputTokens)}
	if u.InputTokensDetails != nil {
		tok.CacheRead = copyInt(u.InputTokensDetails.CachedTokens)
	}
	if u.OutputTokensDetails != nil {
		tok.Reasoning = copyInt(u.OutputTokensDetails.ReasoningTokens)
	}
	if tok.Input == nil && tok.Output == nil && tok.CacheRead == nil && tok.Reasoning == nil {
		return nil
	}
	return tok
}
