package openai

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// chatRequest is the Chat Completions request body — the "OpenAI-compatible"
// wire shape spoken by xAI/Grok, OpenRouter, and local servers. Only the fields
// the adapter sets are modeled. max_tokens (rather than the newer
// max_completion_tokens) is used because it is the field every compatible
// endpoint understands, which is the whole point of this surface.
type chatRequest struct {
	Model         string             `json:"model"`
	Messages      []chatMessage      `json:"messages"`
	Tools         []chatTool         `json:"tools,omitempty"`
	MaxTokens     int                `json:"max_tokens,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	Stop          []string           `json:"stop,omitempty"`
	Stream        bool               `json:"stream"`
	StreamOptions *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatMessage is one Chat Completions message. Content is a string, an array of
// typed parts (multimodal), or omitted (an assistant message carrying only
// tool_calls).
type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTextPart struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

type chatImagePart struct {
	Type     string       `json:"type"` // "image_url"
	ImageURL chatImageURL `json:"image_url"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

type chatTool struct {
	Type     string      `json:"type"` // "function"
	Function chatToolDef `json:"function"`
}

type chatToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// buildChatRequest maps a provider.Request into the Chat Completions wire body.
// Consecutive assistant-authored blocks (text + tool calls) are merged into one
// assistant message, matching the surface's one-message-per-turn shape; tool
// results become tool-role messages keyed by tool_call_id.
func buildChatRequest(req provider.Request) (*chatRequest, error) {
	w := &chatRequest{
		Model:         req.Model,
		MaxTokens:     req.Params.MaxTokens,
		Temperature:   req.Params.Temperature,
		TopP:          req.Params.TopP,
		Stop:          req.Params.StopSequences,
		Stream:        true,
		StreamOptions: &chatStreamOptions{IncludeUsage: true},
	}

	tools, err := buildChatTools(req.Tools)
	if err != nil {
		return nil, err
	}
	w.Tools = tools

	for i := range req.Context {
		b := &req.Context[i]
		switch b.Kind {
		case schema.KindText:
			if b.Role == schema.RoleSystem {
				if t := systemText(b); t != "" {
					w.Messages = append(w.Messages, chatMessage{Role: "system", Content: t})
				}
				continue
			}
			if b.Role == schema.RoleAssistant {
				appendAssistantText(w, b)
				continue
			}
			if c := chatUserContent(b.Text); c != nil {
				w.Messages = append(w.Messages, chatMessage{Role: "user", Content: c})
			}
		case schema.KindCompaction:
			// A compaction block is a derived summary standing in for the
			// conversation it replaced (AS-038); it carries a text body and is built
			// with a user role, so it renders as a user message and reaches the model.
			if c := chatUserContent(b.Text); c != nil {
				w.Messages = append(w.Messages, chatMessage{Role: "user", Content: c})
			}
		case schema.KindToolCall:
			if err := appendAssistantToolCall(w, b); err != nil {
				return nil, err
			}
		case schema.KindToolResult:
			if b.ToolResult != nil {
				w.Messages = append(w.Messages, chatMessage{
					Role:       "tool",
					ToolCallID: b.ToolResult.ToolUseID,
					Content:    toolResultText(b.ToolResult),
				})
			}
		case schema.KindFileRead:
			if b.FileRead != nil {
				w.Messages = append(w.Messages, chatMessage{Role: "user", Content: fileReadText(b.FileRead)})
			}
		default:
			// Reasoning and derived kinds have no Chat Completions input field;
			// the model regenerates reasoning each turn, so they are dropped.
		}
	}

	return w, nil
}

// trailingAssistant returns a pointer to the last message when it is an assistant
// message, so consecutive assistant blocks accumulate into it; otherwise it
// appends a fresh assistant message and returns that.
func trailingAssistant(w *chatRequest) *chatMessage {
	if n := len(w.Messages); n > 0 && w.Messages[n-1].Role == "assistant" {
		return &w.Messages[n-1]
	}
	w.Messages = append(w.Messages, chatMessage{Role: "assistant"})
	return &w.Messages[len(w.Messages)-1]
}

func appendAssistantText(w *chatRequest, b *schema.Block) {
	if b.Text == nil || b.Text.Text == "" {
		return
	}
	m := trailingAssistant(w)
	if existing, ok := m.Content.(string); ok && existing != "" {
		m.Content = existing + "\n" + b.Text.Text
	} else {
		m.Content = b.Text.Text
	}
}

func appendAssistantToolCall(w *chatRequest, b *schema.Block) error {
	body := b.ToolCall
	if body == nil {
		return nil
	}
	args := argumentsString(body)
	if !json.Valid([]byte(args)) {
		return errInvalidToolArgs(body.ToolUseID)
	}
	m := trailingAssistant(w)
	m.ToolCalls = append(m.ToolCalls, chatToolCall{
		ID:       body.ToolUseID,
		Type:     "function",
		Function: chatToolCallFunc{Name: body.Name, Arguments: args},
	})
	return nil
}

// chatUserContent renders a user text body as either a plain string (text only)
// or an array of typed parts (multimodal). Returns nil when there is nothing to
// send.
func chatUserContent(body *schema.TextBody) any {
	if body == nil {
		return nil
	}
	if len(body.Parts) == 0 {
		if body.Text == "" {
			return nil
		}
		return body.Text
	}
	parts := make([]any, 0, len(body.Parts))
	for i := range body.Parts {
		p := &body.Parts[i]
		switch p.Type {
		case "image":
			url := p.URL
			if url == "" && p.Data != "" {
				url = "data:" + p.MediaType + ";base64," + p.Data
			}
			parts = append(parts, chatImagePart{Type: "image_url", ImageURL: chatImageURL{URL: url}})
		default:
			parts = append(parts, chatTextPart{Type: "text", Text: p.Text})
		}
	}
	return parts
}

func fileReadText(body *schema.FileReadBody) string {
	text := body.Content
	if body.Path != "" {
		text = body.Path + ":\n" + body.Content
	}
	return text
}

func buildChatTools(defs []provider.ToolDef) ([]chatTool, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	out := make([]chatTool, 0, len(defs))
	for i := range defs {
		d := &defs[i]
		params, err := toolParameters(d)
		if err != nil {
			return nil, err
		}
		out = append(out, chatTool{
			Type:     "function",
			Function: chatToolDef{Name: d.Name, Description: d.Description, Parameters: params},
		})
	}
	return out, nil
}

// --- streaming --------------------------------------------------------------

// chatStream reads a Chat Completions SSE response and normalizes each chunk
// into the provider package's uniform Event stream. Chat Completions has no
// explicit block framing — blocks are implicit in the deltas — so the stream
// opens a block lazily on the first delta that needs one and closes all open
// blocks when finish_reason arrives. To keep usage (which the API sends in a
// trailing chunk after finish_reason, when stream_options.include_usage is set)
// ahead of the turn stop, the turn-stop event is deferred until the [DONE]
// sentinel or a clean EOF.
type chatStream struct {
	r sseReader

	pending []provider.Event
	cur     provider.Event

	started bool // turn start emitted

	textIdx      int         // block index of the open text block, or -1
	reasoningIdx int         // block index of the open reasoning block, or -1
	toolBlock    map[int]int // chat tool_calls index -> our block index
	openOrder    []int       // open block indices, in open order
	nextIdx      int         // next block index to allocate

	finished    bool   // a finish_reason chunk was seen
	stopReason  string // normalized stop reason, captured at finish
	stopEmitted bool   // turn stop already enqueued

	err    error
	done   bool
	closed bool
}

func newChatStream(body io.ReadCloser) *chatStream {
	return &chatStream{
		r:            newSSEReader(body),
		textIdx:      -1,
		reasoningIdx: -1,
		toolBlock:    map[int]int{},
	}
}

func (s *chatStream) Next() bool {
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
				// Flush a deferred turn stop on a clean end (an endpoint that
				// closes without a [DONE] sentinel). The body is exhausted, so mark
				// the stream done now to avoid a redundant read on the next Next().
				if ev, ok := s.flushStop(); ok {
					s.pending = append(s.pending, ev)
					s.done = true
					continue
				}
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

func (s *chatStream) Event() provider.Event { return s.cur }

func (s *chatStream) Err() error { return s.err }

func (s *chatStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.r.body.Close()
}

// chatChunk is the Chat Completions streaming chunk shape.
type chatChunk struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

type chatChoice struct {
	Delta        chatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

type chatDelta struct {
	Role             string              `json:"role"`
	Content          *string             `json:"content"`
	ReasoningContent *string             `json:"reasoning_content"` // Grok reasoning extension
	ToolCalls        []chatDeltaToolCall `json:"tool_calls"`
}

type chatDeltaToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatUsage is the Chat Completions usage object. Counts are pointers so an
// unreported field stays nil, matching schema.Tokens.
type chatUsage struct {
	PromptTokens        *int `json:"prompt_tokens"`
	CompletionTokens    *int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// translate normalizes one Chat Completions SSE frame into zero or more provider
// Events. The [DONE] sentinel ends the stream; an error chunk terminates it.
func (s *chatStream) translate(data []byte) []provider.Event {
	if string(data) == "[DONE]" {
		s.done = true
		if ev, ok := s.flushStop(); ok {
			return []provider.Event{ev}
		}
		return nil
	}

	var c chatChunk
	if err := json.Unmarshal(data, &c); err != nil {
		e := provider.New(provider.ErrUnknown, "decoding stream chunk: %v", err)
		e.Err = err
		s.err = e
		return nil
	}
	if c.Error != nil {
		s.err = mapChatErrorChunk(c.Error.Type, c.Error.Code, c.Error.Message)
		return nil
	}

	var out []provider.Event
	if !s.started {
		s.started = true
		out = append(out, provider.Event{
			Type: provider.EventTurnStart,
			Turn: &provider.TurnInfo{ResponseID: c.ID, Model: c.Model},
		})
	}

	for i := range c.Choices {
		out = append(out, s.onChoice(&c.Choices[i])...)
	}

	if tok := chatTokens(c.Usage); tok != nil {
		out = append(out, provider.Event{Type: provider.EventUsage, Usage: tok})
	}

	return out
}

func (s *chatStream) onChoice(ch *chatChoice) []provider.Event {
	var out []provider.Event
	d := &ch.Delta

	// Reasoning (Grok reasoning_content) before visible content, mirroring the
	// order providers emit it.
	if d.ReasoningContent != nil && *d.ReasoningContent != "" {
		if s.reasoningIdx < 0 {
			s.reasoningIdx = s.openBlock(&provider.BlockHeader{Kind: schema.KindReasoning, Role: schema.RoleAssistant}, &out)
		}
		out = append(out, provider.Event{Type: provider.EventReasoningDelta, BlockIndex: s.reasoningIdx, TextDelta: *d.ReasoningContent})
	}

	if d.Content != nil && *d.Content != "" {
		if s.textIdx < 0 {
			s.textIdx = s.openBlock(&provider.BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}, &out)
		}
		out = append(out, provider.Event{Type: provider.EventTextDelta, BlockIndex: s.textIdx, TextDelta: *d.Content})
	}

	for i := range d.ToolCalls {
		tc := &d.ToolCalls[i]
		idx, ok := s.toolBlock[tc.Index]
		if !ok {
			idx = s.openBlock(&provider.BlockHeader{
				Kind:      schema.KindToolCall,
				Role:      schema.RoleAssistant,
				ToolUseID: tc.ID,
				ToolName:  tc.Function.Name,
				ToolKind:  schema.ToolKindClient,
			}, &out)
			s.toolBlock[tc.Index] = idx
		}
		if tc.Function.Arguments != "" {
			out = append(out, provider.Event{Type: provider.EventToolCallDelta, BlockIndex: idx, ArgumentsDelta: tc.Function.Arguments})
		}
	}

	if ch.FinishReason != "" {
		out = append(out, s.closeOpenBlocks()...)
		s.finished = true
		s.stopReason = mapChatFinishReason(ch.FinishReason)
	}

	return out
}

// openBlock allocates a block index, appends a block-start event to out, and
// records the index as open.
func (s *chatStream) openBlock(header *provider.BlockHeader, out *[]provider.Event) int {
	idx := s.nextIdx
	s.nextIdx++
	s.openOrder = append(s.openOrder, idx)
	*out = append(*out, provider.Event{Type: provider.EventBlockStart, BlockIndex: idx, Header: header})
	return idx
}

// closeOpenBlocks emits a block-stop for every open block, in open order, and
// clears the open-block bookkeeping.
func (s *chatStream) closeOpenBlocks() []provider.Event {
	out := make([]provider.Event, 0, len(s.openOrder))
	for _, idx := range s.openOrder {
		out = append(out, provider.Event{Type: provider.EventBlockStop, BlockIndex: idx})
	}
	s.openOrder = nil
	s.textIdx, s.reasoningIdx = -1, -1
	s.toolBlock = map[int]int{}
	return out
}

// flushStop returns the deferred turn-stop event once, after a finish_reason was
// seen.
func (s *chatStream) flushStop() (provider.Event, bool) {
	if !s.finished || s.stopEmitted {
		return provider.Event{}, false
	}
	s.stopEmitted = true
	return provider.Event{Type: provider.EventTurnStop, StopReason: s.stopReason}, true
}

// mapChatFinishReason maps a Chat Completions finish_reason onto the normalized
// Stop* set.
func mapChatFinishReason(r string) string {
	switch r {
	case "stop":
		return provider.StopEndTurn
	case "length":
		return provider.StopMaxTokens
	case "tool_calls", "function_call":
		return provider.StopToolUse
	case "content_filter":
		return provider.StopContentFilter
	default:
		return r
	}
}

func chatTokens(u *chatUsage) *schema.Tokens {
	if u == nil {
		return nil
	}
	tok := &schema.Tokens{Input: copyInt(u.PromptTokens), Output: copyInt(u.CompletionTokens)}
	if u.PromptTokensDetails != nil {
		tok.CacheRead = copyInt(u.PromptTokensDetails.CachedTokens)
	}
	if u.CompletionTokensDetails != nil {
		tok.Reasoning = copyInt(u.CompletionTokensDetails.ReasoningTokens)
	}
	if tok.Input == nil && tok.Output == nil && tok.CacheRead == nil && tok.Reasoning == nil {
		return nil
	}
	return tok
}
