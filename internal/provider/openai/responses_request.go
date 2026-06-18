package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// responsesRequest is the OpenAI Responses API request body. Only the fields the
// adapter sets are modeled; everything else defers to the API's defaults.
type responsesRequest struct {
	Model           string              `json:"model"`
	Instructions    string              `json:"instructions,omitempty"`
	Input           []any               `json:"input"`
	Tools           []responsesTool     `json:"tools,omitempty"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
	Metadata        map[string]string   `json:"metadata,omitempty"`
	Store           bool                `json:"store"`
	Stream          bool                `json:"stream"`
}

// responsesReasoning requests extended reasoning. Effort maps from
// ReasoningOpts.Effort; summary "auto" asks the API to stream a reasoning
// summary the adapter can surface as a reasoning block.
type responsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// responsesTool is a Responses-API tool definition. Unlike Chat Completions, the
// function fields are flat (no nested "function" object).
type responsesTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Input item shapes (a typed list; "type" discriminates on the wire).

type responsesMessage struct {
	Type    string `json:"type"` // "message"
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type responsesInputText struct {
	Type string `json:"type"` // "input_text"
	Text string `json:"text"`
}

type responsesOutputText struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

type responsesInputImage struct {
	Type     string `json:"type"`      // "input_image"
	ImageURL string `json:"image_url"` // URL or data: URI
	Detail   string `json:"detail,omitempty"`
}

type responsesFunctionCall struct {
	Type      string `json:"type"` // "function_call"
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string (Responses arguments are a string)
}

type responsesFunctionCallOutput struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type responsesReasoningItem struct {
	Type             string                 `json:"type"` // "reasoning"
	ID               string                 `json:"id,omitempty"`
	Summary          []responsesSummaryText `json:"summary"`
	EncryptedContent string                 `json:"encrypted_content,omitempty"`
}

type responsesSummaryText struct {
	Type string `json:"type"` // "summary_text"
	Text string `json:"text"`
}

// buildResponsesRequest maps a provider.Request into the Responses wire body.
// System blocks (schema.RoleSystem) are concatenated into the top-level
// instructions field (keeping the projection the single source of truth, PRD
// D3); the rest become typed input items.
func buildResponsesRequest(req provider.Request) (*responsesRequest, error) {
	w := &responsesRequest{
		Model:           req.Model,
		Input:           []any{},
		MaxOutputTokens: req.Params.MaxTokens, // 0 omitted -> provider default
		Temperature:     req.Params.Temperature,
		TopP:            req.Params.TopP,
		Metadata:        req.Metadata,
		Store:           false, // stateless: we own the log, not the server (D3)
		Stream:          true,
	}
	if r := req.Params.Reasoning; r != nil && r.Effort != "" {
		w.Reasoning = &responsesReasoning{Effort: r.Effort, Summary: "auto"}
	}
	seenToolCalls := make(map[string]bool)

	tools, err := buildResponsesTools(req.Tools)
	if err != nil {
		return nil, err
	}
	w.Tools = tools

	var system []string
	for i := range req.Context {
		b := &req.Context[i]
		if b.Role == schema.RoleSystem {
			if t := systemText(b); t != "" {
				system = append(system, t)
			}
			continue
		}
		var items []any
		switch b.Kind {
		case schema.KindToolCall:
			items, err = responsesToolCallItem(b)
			if err != nil {
				return nil, err
			}
			if b.ToolCall != nil && b.ToolCall.ToolUseID != "" {
				seenToolCalls[b.ToolCall.ToolUseID] = true
			}
		case schema.KindToolResult:
			if b.ToolResult != nil && seenToolCalls[b.ToolResult.ToolUseID] {
				items = responsesToolResultItem(b)
			} else {
				items = responsesOrphanToolResultItem(b)
			}
		default:
			items, err = responsesInputFor(b)
			if err != nil {
				return nil, err
			}
		}
		w.Input = append(w.Input, items...)
	}
	w.Instructions = strings.Join(system, "\n\n")

	return w, nil
}

// systemText renders a system block's text, falling back to empty rather than
// erroring so an unusual shape never blocks a turn.
func systemText(b *schema.Block) string {
	if b.Text != nil {
		return b.Text.Text
	}
	return ""
}

// responsesInputFor converts one log block into zero or more Responses input
// items. Unknown/derived kinds carry no model-facing content and are skipped.
func responsesInputFor(b *schema.Block) ([]any, error) {
	switch b.Kind {
	case schema.KindText, schema.KindCompaction:
		// A compaction block is a derived summary standing in for the conversation
		// it replaced (AS-038); it carries a text body, so it renders as a message
		// item and reaches the model.
		return responsesTextItem(b), nil
	case schema.KindReasoning:
		return responsesReasoningInput(b), nil
	case schema.KindToolCall:
		return responsesToolCallItem(b)
	case schema.KindToolResult:
		return responsesToolResultItem(b), nil
	case schema.KindFileRead:
		return responsesFileReadItem(b), nil
	default:
		return nil, nil
	}
}

// responsesTextItem renders a text block as a message item. Assistant text uses
// output_text parts; user (and any other) text uses input_text. Multimodal parts
// are preserved in order.
func responsesTextItem(b *schema.Block) []any {
	body := b.Text
	if body == nil {
		return nil
	}
	role := responsesRole(b.Role)
	assistant := b.Role == schema.RoleAssistant

	var content []any
	if len(body.Parts) > 0 {
		for i := range body.Parts {
			content = append(content, partToResponses(&body.Parts[i], assistant))
		}
	} else if body.Text != "" {
		content = append(content, textPart(body.Text, assistant))
	}
	if len(content) == 0 {
		return nil
	}
	return []any{responsesMessage{Type: "message", Role: role, Content: content}}
}

func textPart(text string, assistant bool) any {
	if assistant {
		return responsesOutputText{Type: "output_text", Text: text}
	}
	return responsesInputText{Type: "input_text", Text: text}
}

// partToResponses converts a multimodal Part into its Responses content shape.
// Image parts become input_image; any other part degrades to text so nothing is
// silently dropped.
func partToResponses(p *schema.Part, assistant bool) any {
	switch p.Type {
	case "image":
		url := p.URL
		if url == "" && p.Data != "" {
			url = "data:" + p.MediaType + ";base64," + p.Data
		}
		return responsesInputImage{Type: "input_image", ImageURL: url}
	default:
		return textPart(p.Text, assistant)
	}
}

// responsesReasoningInput re-emits a reasoning block for stateless reuse. OpenAI
// reuses reasoning via encrypted_content (+ the item id), so the adapter only
// re-sends a reasoning item when it carries encrypted content — visible-only
// summaries are regenerated by the model and re-feeding them as bare reasoning
// items risks a malformed-input error.
func responsesReasoningInput(b *schema.Block) []any {
	body := b.Reasoning
	if body == nil || body.Encrypted == "" {
		return nil
	}
	item := responsesReasoningItem{Type: "reasoning", EncryptedContent: body.Encrypted, Summary: []responsesSummaryText{}}
	if b.Provider != nil && b.Provider.NativeID != "" {
		item.ID = b.Provider.NativeID
	}
	for _, s := range body.Summary {
		item.Summary = append(item.Summary, responsesSummaryText{Type: "summary_text", Text: s})
	}
	return []any{item}
}

func responsesToolCallItem(b *schema.Block) ([]any, error) {
	body := b.ToolCall
	if body == nil {
		return nil, nil
	}
	args := argumentsString(body)
	if !json.Valid([]byte(args)) {
		return nil, fmt.Errorf("tool_call %q has invalid JSON arguments", body.ToolUseID)
	}
	return []any{responsesFunctionCall{
		Type:      "function_call",
		CallID:    body.ToolUseID,
		Name:      body.Name,
		Arguments: args,
	}}, nil
}

func responsesToolResultItem(b *schema.Block) []any {
	body := b.ToolResult
	if body == nil {
		return nil
	}
	return []any{responsesFunctionCallOutput{
		Type:   "function_call_output",
		CallID: body.ToolUseID,
		Output: toolResultText(body),
	}}
}

// responsesOrphanToolResultItem renders a tool result as plain user-visible text
// when its paired tool_call is no longer in the projected context. The
// Responses API rejects a bare function_call_output with no matching call in the
// same request, so degraded text is safer than emitting an invalid shape.
func responsesOrphanToolResultItem(b *schema.Block) []any {
	body := b.ToolResult
	if body == nil {
		return nil
	}
	text := toolResultText(body)
	if text == "" {
		text = "(empty tool result)"
	}
	return []any{responsesMessage{
		Type: "message",
		Role: "user",
		Content: []any{responsesInputText{
			Type: "input_text",
			Text: "Tool result:\n" + text,
		}},
	}}
}

// responsesFileReadItem renders a harness-native file_read block as a plain user
// text item so its content reaches the model without inventing a function call
// output whose paired call may not be in the projection (mirrors the anthropic
// adapter's handling).
func responsesFileReadItem(b *schema.Block) []any {
	body := b.FileRead
	if body == nil {
		return nil
	}
	text := body.Content
	if body.Path != "" {
		text = body.Path + ":\n" + body.Content
	}
	return []any{responsesMessage{
		Type:    "message",
		Role:    "user",
		Content: []any{responsesInputText{Type: "input_text", Text: text}},
	}}
}

// responsesRole maps a schema role to the Responses role model. Tool results are
// carried as their own function_call_output items, not messages, so only
// assistant/user/system reach this; anything unexpected defaults to user.
func responsesRole(r schema.Role) string {
	switch r {
	case schema.RoleAssistant:
		return "assistant"
	case schema.RoleSystem:
		return "system"
	default:
		return "user"
	}
}

// buildResponsesTools renders offered tool definitions into Responses tool
// shapes. An empty input schema defaults to an open object schema.
func buildResponsesTools(defs []provider.ToolDef) ([]responsesTool, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	out := make([]responsesTool, 0, len(defs))
	for i := range defs {
		d := &defs[i]
		params, err := toolParameters(d)
		if err != nil {
			return nil, err
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        d.Name,
			Description: d.Description,
			Parameters:  params,
		})
	}
	return out, nil
}
