package schema

import "encoding/json"

// TextBody is the body of a KindText block (union §6.1). Text is optional so a
// purely multimodal turn (only Parts) is representable; a refusal is a text
// block with Subtype "refusal" and StopReason "refusal" on the envelope, the
// original payload preserved in Ext.
type TextBody struct {
	Text        string                     `json:"text,omitempty"`
	Subtype     string                     `json:"subtype,omitempty"`     // normal|refusal
	Parts       []Part                     `json:"parts,omitempty"`       // multimodal input parts, order preserved
	Citations   []Citation                 `json:"citations,omitempty"`   // Anthropic, Grok Live Search
	Annotations []json.RawMessage          `json:"annotations,omitempty"` // OpenAI Responses annotations (opaque)
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`
}

// ToolCallBody is the body of a KindToolCall block (union §6.2). Arguments is
// canonical structured JSON; ArgumentsRaw keeps the verbatim string when a
// surface sent one, because signatures/caching depend on exact bytes. ToolUseID
// is the union key linking a call to its result.
type ToolCallBody struct {
	ToolUseID     string                     `json:"tool_use_id"`
	Name          string                     `json:"name"`
	Arguments     json.RawMessage            `json:"arguments,omitempty"`      // structured JSON object
	ArgumentsRaw  string                     `json:"arguments_raw,omitempty"`  // verbatim string when provided
	ToolKind      string                     `json:"tool_kind,omitempty"`      // client|server
	ToolSubtype   string                     `json:"tool_subtype,omitempty"`   // specific server-tool name
	ParallelGroup string                     `json:"parallel_group,omitempty"` // parallel tool calls (AS-019)
	MCPServer     string                     `json:"mcp_server,omitempty"`
	Ext           map[string]json.RawMessage `json:"ext,omitempty"`
}

// ToolResultBody is the body of a KindToolResult block (union §6.3), one per
// ToolUseID. Server-tool results that a surface fuses onto the call are split
// into a paired tool_call + tool_result, linked by provenance. IsError is
// first-class; for surfaces without it, infer from a non-zero ExitCode and
// record the raw signal in Ext.
type ToolResultBody struct {
	ToolUseID         string                     `json:"tool_use_id"`
	Content           []Part                     `json:"content,omitempty"` // typed result parts
	IsError           bool                       `json:"is_error,omitempty"`
	Citations         []Citation                 `json:"citations,omitempty"`
	ExitCode          *int                       `json:"exit_code,omitempty"` // pointer: 0 is meaningful, nil is unreported
	Stdout            string                     `json:"stdout,omitempty"`
	Stderr            string                     `json:"stderr,omitempty"`
	StructuredContent json.RawMessage            `json:"structured_content,omitempty"`
	Truncated         bool                       `json:"truncated,omitempty"`
	OffloadRef        string                     `json:"offload_ref,omitempty"` // pointer to out-of-log storage for large results
	Ext               map[string]json.RawMessage `json:"ext,omitempty"`
}

// FileReadBody is the body of a KindFileRead block (union §6.4) — an Agent
// Smith-native block. Content and ContentHash are optional to cover
// large/binary/offloaded content and failed reads, where the attempt and
// metadata are still logged.
type FileReadBody struct {
	Path        string                     `json:"path"`
	Range       *LineRange                 `json:"range,omitempty"` // nil = whole file
	Content     string                     `json:"content,omitempty"`
	ContentHash string                     `json:"content_hash,omitempty"` // for dedupe (composition view)
	OffloadRef  string                     `json:"offload_ref,omitempty"`  // out-of-log pointer when content is too large/binary
	Error       string                     `json:"error,omitempty"`        // records a failed read
	ProducedBy  string                     `json:"produced_by,omitempty"`  // tool_use_id of the read call, if any
	MediaType   string                     `json:"media_type,omitempty"`   // text/image/pdf/...
	Source      string                     `json:"source,omitempty"`       // tool|attachment|mcp_resource
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`
}

// LineRange is a line or byte range within a file (union §6.4). A nil *LineRange
// means the whole file.
type LineRange struct {
	StartLine int `json:"start_line,omitempty"`
	EndLine   int `json:"end_line,omitempty"`
	StartByte int `json:"start_byte,omitempty"`
	EndByte   int `json:"end_byte,omitempty"`
}

// ReasoningBody is the body of a KindReasoning block (union §6.5). Encrypted is
// opaque passthrough (Anthropic redacted_thinking, OpenAI encrypted_content,
// Grok encrypted thinking) and is never inspected. ReplayScope and Signature
// let the projection engine (AS-006) honor each provider's replay contract.
type ReasoningBody struct {
	Text        string                     `json:"text,omitempty"`      // visible/summarized reasoning (may be empty)
	Summary     []string                   `json:"summary,omitempty"`   // summary parts
	Encrypted   string                     `json:"encrypted,omitempty"` // opaque, stored verbatim, never inspected
	Signature   string                     `json:"signature,omitempty"` // Anthropic thinking signature (replay integrity)
	Redacted    bool                       `json:"redacted,omitempty"`
	ReplayScope string                     `json:"replay_scope,omitempty"` // same_model_only|portable (default portable when absent)
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`
}

// Part is one piece of multimodal content shared by text inputs and tool
// results (union §6.1, §6.3, §10). Order within a slice is significant.
type Part struct {
	Type       string                     `json:"type"` // text|image|audio|file|...
	MediaType  string                     `json:"media_type,omitempty"`
	Text       string                     `json:"text,omitempty"`
	URL        string                     `json:"url,omitempty"`
	Data       string                     `json:"data,omitempty"`        // inline base64
	OffloadRef string                     `json:"offload_ref,omitempty"` // out-of-log pointer for large/binary parts
	Ext        map[string]json.RawMessage `json:"ext,omitempty"`
}

// Citation is a cited source attached to a text or tool_result block (union
// §6.3) — shared by Anthropic citations and Grok Live Search.
type Citation struct {
	Type      string                     `json:"type,omitempty"`
	URL       string                     `json:"url,omitempty"`
	Title     string                     `json:"title,omitempty"`
	CitedText string                     `json:"cited_text,omitempty"`
	Source    string                     `json:"source,omitempty"`
	Ext       map[string]json.RawMessage `json:"ext,omitempty"`
}
