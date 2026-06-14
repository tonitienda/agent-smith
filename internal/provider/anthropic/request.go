package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// wireRequest is the Anthropic Messages API request body. Only the fields the
// adapter sets are modeled; everything else defers to the API's defaults.
type wireRequest struct {
	Model         string         `json:"model"`
	MaxTokens     int            `json:"max_tokens"`
	System        []wireText     `json:"system,omitempty"`
	Messages      []wireMessage  `json:"messages"`
	Tools         []wireTool     `json:"tools,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Thinking      *wireThinking  `json:"thinking,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Stream        bool           `json:"stream"`
}

// wireMessage is one Messages-API message: a role and an ordered list of content
// blocks. Content is heterogeneous (text, tool_use, tool_result, thinking…), so
// each element is an already-shaped content struct marshaled in order.
type wireMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type wireCacheControl struct {
	Type string `json:"type"`          // always "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "5m" | "1h"; empty defers to the API default
}

type wireText struct {
	Type         string            `json:"type"` // "text"
	Text         string            `json:"text"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireToolUse struct {
	Type         string            `json:"type"` // "tool_use"
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Input        json.RawMessage   `json:"input"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireToolResult struct {
	Type         string            `json:"type"` // "tool_result"
	ToolUseID    string            `json:"tool_use_id"`
	Content      any               `json:"content"` // string or []wire content blocks
	IsError      bool              `json:"is_error,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireThinkingBlock struct {
	Type      string `json:"type"` // "thinking"
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

type wireRedactedThinking struct {
	Type string `json:"type"` // "redacted_thinking"
	Data string `json:"data"`
}

type wireImageSource struct {
	Type      string `json:"type"`                 // "base64" | "url"
	MediaType string `json:"media_type,omitempty"` // base64 only
	Data      string `json:"data,omitempty"`       // base64 only
	URL       string `json:"url,omitempty"`        // url only
}

type wireImage struct {
	Type         string            `json:"type"` // "image"
	Source       wireImageSource   `json:"source"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireTool struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	InputSchema  json.RawMessage   `json:"input_schema"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireThinking struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}

// buildWireRequest maps a provider.Request into the Messages wire body. System
// blocks (schema.RoleSystem) are extracted into the top-level system field
// (keeping the projection the single source of truth, PRD D3); the rest are
// grouped into role-alternating messages. Cache breakpoints (req.Cache) attach
// cache_control to the matching block by its schema ID.
func buildWireRequest(req provider.Request, defaultMaxTokens int) (*wireRequest, error) {
	breakpoints := breakpointSet(req.Cache, req.Context)

	maxTokens := req.Params.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	w := &wireRequest{
		Model:         req.Model,
		MaxTokens:     maxTokens,
		Stream:        true,
		Temperature:   req.Params.Temperature,
		TopP:          req.Params.TopP,
		StopSequences: req.Params.StopSequences,
	}
	if r := req.Params.Reasoning; r != nil && r.BudgetTokens > 0 {
		w.Thinking = &wireThinking{Type: "enabled", BudgetTokens: r.BudgetTokens}
	}
	if len(req.Metadata) > 0 {
		w.Metadata = make(map[string]any, len(req.Metadata))
		for k, v := range req.Metadata {
			w.Metadata[k] = v
		}
	}

	tools, err := buildTools(req.Tools)
	if err != nil {
		return nil, err
	}
	w.Tools = tools

	for i := range req.Context {
		b := &req.Context[i]
		if b.Role == schema.RoleSystem {
			w.System = append(w.System, wireText{
				Type:         "text",
				Text:         systemText(b),
				CacheControl: cacheControlFor(b, breakpoints),
			})
			continue
		}

		content, err := contentFor(b, breakpoints)
		if err != nil {
			return nil, err
		}
		if len(content) == 0 {
			continue
		}

		role := wireRole(b.Role)
		// Merge consecutive blocks that map to the same wire role into one
		// message: Anthropic expects user/assistant messages, each carrying an
		// ordered list of content blocks (tool_use under assistant, tool_result
		// under user), not one message per block.
		if n := len(w.Messages); n > 0 && w.Messages[n-1].Role == role {
			w.Messages[n-1].Content = append(w.Messages[n-1].Content, content...)
		} else {
			w.Messages = append(w.Messages, wireMessage{Role: role, Content: content})
		}
	}

	return w, nil
}

// wireRole maps a schema role to the Messages API's two-role model: assistant
// for assistant-authored blocks, user for everything else (user input and
// tool_result blocks, which Anthropic carries inside a user message).
func wireRole(r schema.Role) string {
	if r == schema.RoleAssistant {
		return "assistant"
	}
	return "user"
}

// systemText renders a system block's text. A system block is a text block;
// fall back to an empty string rather than erroring so an unusual shape never
// blocks a turn.
func systemText(b *schema.Block) string {
	if b.Text != nil {
		return b.Text.Text
	}
	return ""
}

// contentFor converts one log block into zero or more Messages content blocks.
// It returns nil for blocks that carry nothing to send (the caller skips them).
func contentFor(b *schema.Block, breakpoints map[string]string) ([]any, error) {
	cc := cacheControlFor(b, breakpoints)
	switch b.Kind {
	case schema.KindText:
		return textContent(b, cc), nil
	case schema.KindReasoning:
		return reasoningContent(b), nil
	case schema.KindToolCall:
		return toolCallContent(b, cc)
	case schema.KindToolResult:
		return toolResultContent(b, cc), nil
	case schema.KindFileRead:
		return fileReadContent(b, cc), nil
	default:
		// Unknown/derived kinds (compaction, fallback markers) carry no
		// model-facing content of their own; skip them rather than guess a shape.
		return nil, nil
	}
}

func textContent(b *schema.Block, cc *wireCacheControl) []any {
	body := b.Text
	if body == nil {
		return nil
	}
	// A purely multimodal block carries only Parts; otherwise a single text block.
	if len(body.Parts) > 0 {
		out := make([]any, 0, len(body.Parts))
		for i := range body.Parts {
			out = append(out, partToWire(&body.Parts[i]))
		}
		// Attach the cache breakpoint to the final part of the block, whether it
		// is text or an image (Anthropic supports cache_control on image blocks,
		// which are large and worth caching).
		if cc != nil {
			switch t := out[len(out)-1].(type) {
			case wireText:
				t.CacheControl = cc
				out[len(out)-1] = t
			case wireImage:
				t.CacheControl = cc
				out[len(out)-1] = t
			}
		}
		return out
	}
	return []any{wireText{Type: "text", Text: body.Text, CacheControl: cc}}
}

func reasoningContent(b *schema.Block) []any {
	body := b.Reasoning
	if body == nil {
		return nil
	}
	// Redacted/encrypted thinking round-trips verbatim as a redacted_thinking
	// block; visible thinking as a thinking block with its replay signature.
	if body.Redacted || body.Encrypted != "" {
		return []any{wireRedactedThinking{Type: "redacted_thinking", Data: body.Encrypted}}
	}
	if body.Text == "" && body.Signature == "" {
		return nil
	}
	return []any{wireThinkingBlock{Type: "thinking", Thinking: body.Text, Signature: body.Signature}}
}

func toolCallContent(b *schema.Block, cc *wireCacheControl) ([]any, error) {
	body := b.ToolCall
	if body == nil {
		return nil, nil
	}
	input := body.Arguments
	if len(input) == 0 {
		if body.ArgumentsRaw != "" {
			input = json.RawMessage(body.ArgumentsRaw)
		} else {
			input = json.RawMessage(`{}`)
		}
	}
	if !json.Valid(input) {
		return nil, fmt.Errorf("tool_call %q has invalid JSON arguments", body.ToolUseID)
	}
	return []any{wireToolUse{
		Type:         "tool_use",
		ID:           body.ToolUseID,
		Name:         body.Name,
		Input:        input,
		CacheControl: cc,
	}}, nil
}

func toolResultContent(b *schema.Block, cc *wireCacheControl) []any {
	body := b.ToolResult
	if body == nil {
		return nil
	}
	tr := wireToolResult{
		Type:         "tool_result",
		ToolUseID:    body.ToolUseID,
		IsError:      body.IsError,
		CacheControl: cc,
	}
	switch {
	case len(body.Content) > 0:
		parts := make([]any, 0, len(body.Content))
		for i := range body.Content {
			parts = append(parts, partToWire(&body.Content[i]))
		}
		tr.Content = parts
	case body.Stdout != "" || body.Stderr != "":
		tr.Content = body.Stdout + body.Stderr
	default:
		tr.Content = ""
	}
	return []any{tr}
}

// fileReadContent renders a harness-native file_read block (no Anthropic
// analogue) as a plain text content block so its content reaches the model
// without inventing a tool_result whose paired tool_use may not be present in
// the projection. Proper back-projection onto a read-tool tool_result is a
// projection concern (AS-006), not the wire adapter's.
func fileReadContent(b *schema.Block, cc *wireCacheControl) []any {
	body := b.FileRead
	if body == nil {
		return nil
	}
	text := body.Content
	if body.Path != "" {
		text = body.Path + ":\n" + body.Content
	}
	return []any{wireText{Type: "text", Text: text, CacheControl: cc}}
}

// partToWire converts a multimodal Part into its Messages content shape. Text
// parts become text blocks; image parts become image blocks (base64 or url).
// Any other part type degrades to a text block carrying whatever text it holds,
// so nothing is silently dropped.
func partToWire(p *schema.Part) any {
	switch p.Type {
	case "image":
		if p.URL != "" {
			return wireImage{Type: "image", Source: wireImageSource{Type: "url", URL: p.URL}}
		}
		return wireImage{Type: "image", Source: wireImageSource{Type: "base64", MediaType: p.MediaType, Data: p.Data}}
	default:
		return wireText{Type: "text", Text: p.Text}
	}
}

// buildTools renders the offered tool definitions into Messages tool shapes. An
// empty input schema defaults to an open object schema, which the API requires.
func buildTools(defs []provider.ToolDef) ([]wireTool, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	out := make([]wireTool, 0, len(defs))
	for i := range defs {
		d := &defs[i]
		schemaJSON := d.InputSchema
		if len(schemaJSON) == 0 {
			schemaJSON = json.RawMessage(`{"type":"object"}`)
		} else if !json.Valid(schemaJSON) {
			return nil, fmt.Errorf("tool %q has invalid input schema", d.Name)
		}
		out = append(out, wireTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schemaJSON,
		})
	}
	return out, nil
}

// breakpointSet indexes the request's cache breakpoints by block ID, mapping to
// the breakpoint's TTL (possibly ""). It returns nil when there are none.
//
// Placement follows the CacheHints contract (cache-aware assembly, AS-011):
// caching is on by default. Explicit Breakpoints take over placement; otherwise
// the adapter auto-places sensible breakpoints over ctx (autoBreakpoints);
// hints.Disabled opts out entirely.
func breakpointSet(hints provider.CacheHints, ctx []schema.Block) map[string]string {
	if hints.Disabled {
		return nil
	}
	bps := hints.Breakpoints
	if len(bps) == 0 {
		bps = autoBreakpoints(ctx)
	}
	if len(bps) == 0 {
		return nil
	}
	m := make(map[string]string, len(bps))
	for _, bp := range bps {
		if bp.BlockID != "" {
			m[bp.BlockID] = bp.TTL
		}
	}
	return m
}

// autoBreakpoints chooses sensible default cache breakpoints for ctx when the
// caller supplies none. Anthropic caches the prefix up to and including each
// breakpoint (in tools → system → messages order), so two breakpoints cover the
// two prefixes that recur turn to turn:
//
//   - the last system block, which caches the stable tools + system prefix; and
//   - the last block overall, which caches the whole conversation prefix.
//
// Because the log is append-only and the projection preserves block order
// (prefix stability, see internal/projection), the breakpoint placed on this
// turn's final block makes that exact prefix a cache read on the next turn,
// while the new tail is written — the standard incremental-conversation pattern.
// Anthropic ignores cache_control on a prefix shorter than its minimum cacheable
// size, so a tiny context simply no-ops. Returns nil when ctx has no block that
// can carry a breakpoint.
func autoBreakpoints(ctx []schema.Block) []schema.CacheBreakpoint {
	lastSystem, lastBlock := "", ""
	for i := range ctx {
		id := ctx[i].ID
		if id == "" {
			continue
		}
		lastBlock = id
		if ctx[i].Role == schema.RoleSystem {
			lastSystem = id
		}
	}
	var bps []schema.CacheBreakpoint
	if lastSystem != "" {
		bps = append(bps, schema.CacheBreakpoint{BlockID: lastSystem})
	}
	if lastBlock != "" && lastBlock != lastSystem {
		bps = append(bps, schema.CacheBreakpoint{BlockID: lastBlock})
	}
	return bps
}

// cacheControlFor returns the cache_control marker for a block when a breakpoint
// targets its ID, or nil otherwise.
func cacheControlFor(b *schema.Block, breakpoints map[string]string) *wireCacheControl {
	if breakpoints == nil {
		return nil
	}
	ttl, ok := breakpoints[b.ID]
	if !ok {
		return nil
	}
	return &wireCacheControl{Type: "ephemeral", TTL: ttl}
}
