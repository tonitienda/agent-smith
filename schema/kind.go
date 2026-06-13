package schema

// Kind enumerates the content-block kinds of the substrate (PRD D3). The five
// content kinds are frozen at V1; additional kinds (e.g. the derived-block
// kinds below) are added additively and consumers tolerate kinds they do not
// recognize (PRD D2).
type Kind string

const (
	// KindText is a contiguous assistant/user/system text span (union §6.1).
	// A purely multimodal turn is a text block with Text empty and only
	// TextBody.Parts populated.
	KindText Kind = "text"
	// KindToolCall is a request to invoke a tool (union §6.2).
	KindToolCall Kind = "tool_call"
	// KindToolResult is the result of a tool invocation, keyed to its call by
	// ToolUseID (union §6.3).
	KindToolResult Kind = "tool_result"
	// KindFileRead is an Agent Smith-native block recording a file/resource
	// read (union §6.4); no provider exposes it natively, but it back-projects
	// onto a read-tool tool_result.
	KindFileRead Kind = "file_read"
	// KindReasoning is a model reasoning/thinking span (union §6.5). Encrypted
	// reasoning is carried as opaque passthrough and never inspected.
	KindReasoning Kind = "reasoning"

	// KindCompaction is a derived block holding a server-side or harness
	// summary (union §10). It uses the same envelope with
	// Provenance.DerivedFrom linking its sources. Anticipated and additive.
	KindCompaction Kind = "compaction"
	// KindFallback is a derived audit marker recording a server-side model
	// switch (union §10). Anticipated and additive.
	KindFallback Kind = "fallback"
)

// contentKinds is the set of frozen V1 content-block kinds, each of which has a
// dedicated typed body. Used by validation.
var contentKinds = map[Kind]bool{
	KindText:       true,
	KindToolCall:   true,
	KindToolResult: true,
	KindFileRead:   true,
	KindReasoning:  true,
}

// IsContentKind reports whether k is one of the five frozen V1 content kinds.
func (k Kind) IsContentKind() bool { return contentKinds[k] }

// Role identifies the origin of a block on the event log. It unifies the
// provider role models — including OpenAI's developer/system split and
// Anthropic mid-conversation system messages — plus harness-originated events.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
	RoleHarness   Role = "harness"
)

// Tool-execution location for a tool_call (union §6.2).
const (
	ToolKindClient = "client" // the harness executes the tool
	ToolKindServer = "server" // the provider executes the tool (e.g. web search)
)

// Text subtypes (union §6.1).
const (
	TextSubtypeNormal  = "normal"
	TextSubtypeRefusal = "refusal"
)

// Reasoning replay scopes (union §6.5). Drives whether the projection engine
// re-sends a reasoning block across models or drops it.
const (
	// ReplaySameModelOnly: echo unchanged on the same model, drop on others
	// (Anthropic thinking blocks).
	ReplaySameModelOnly = "same_model_only"
	// ReplayPortable: reusable across models (the default when unspecified).
	ReplayPortable = "portable"
)

// Cache modes (union §9).
const (
	CacheModeExplicit  = "explicit"  // client-placed breakpoints (Anthropic)
	CacheModeAutomatic = "automatic" // server-side prefix caching (OpenAI/Grok)
)

// file_read sources (union §6.4).
const (
	FileSourceTool        = "tool"
	FileSourceAttachment  = "attachment"
	FileSourceMCPResource = "mcp_resource"
)
