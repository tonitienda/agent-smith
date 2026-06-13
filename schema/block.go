package schema

import (
	"encoding/json"
	"time"
)

// Block is one event on the append-only log: a shared envelope (union §3) plus
// exactly one typed body matching Kind. History is immutable — edits are
// appended as new blocks (exclusions, derived blocks), never mutations of an
// existing one.
//
// Every optional field uses omitempty so that an absent value never appears as
// a misleading zero, and every pointer/optional honors the "missing means
// unreported, never zero" rule for usage data (union §8).
type Block struct {
	// Envelope (union §3).
	ID          string                     `json:"id"`                    // stable, unique, never reused; ours, not the provider's
	Kind        Kind                       `json:"kind"`                  // discriminates the body
	Seq         int                        `json:"seq"`                   // monotonic append order within a session
	TS          time.Time                  `json:"ts"`                    // append time (harness clock), RFC3339
	Role        Role                       `json:"role"`                  // user|assistant|system|tool|harness
	StopReason  string                     `json:"stop_reason,omitempty"` // turn stop reason (union §7)
	Provenance  *Provenance                `json:"provenance,omitempty"`  // links derived blocks back to sources
	Provider    *Provider                  `json:"provider,omitempty"`    // round-trip fidelity for the source surface
	Thread      *Thread                    `json:"thread,omitempty"`      // sub-agent / multi-agent structure (union §5A)
	Attribution *Attribution               `json:"attribution,omitempty"` // what produced this block (skill/MCP/tool/hook)
	Tokens      *Tokens                    `json:"tokens,omitempty"`      // usage breakdown, fillable later by accounting
	CostUSD     *float64                   `json:"cost_usd,omitempty"`    // filled by accounting
	UsageMeta   *UsageMeta                 `json:"usage_meta,omitempty"`  // service tier / speed / server-tool counts
	Cache       *Cache                     `json:"cache,omitempty"`       // prompt-caching semantics (union §9)
	ExcludedBy  []string                   `json:"excluded_by,omitempty"` // IDs of events that drop this block from the projection
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`         // forward-compat escape hatch

	// Body — exactly one is set, matching Kind.
	Text       *TextBody       `json:"text,omitempty"`
	ToolCall   *ToolCallBody   `json:"tool_call,omitempty"`
	ToolResult *ToolResultBody `json:"tool_result,omitempty"`
	FileRead   *FileReadBody   `json:"file_read,omitempty"`
	Reasoning  *ReasoningBody  `json:"reasoning,omitempty"`
}

// Provenance links a block to the request/turn that produced it and, for
// derived blocks (/clean, /tidy, /compact, compaction), to the source blocks it
// was computed from (union §3) — making reversibility and audit structural.
type Provenance struct {
	Producer    string                     `json:"producer,omitempty"` // who appended it (provider adapter, command, sub-agent)
	RequestID   string                     `json:"request_id,omitempty"`
	ResponseID  string                     `json:"response_id,omitempty"`
	TurnID      string                     `json:"turn_id,omitempty"`
	DerivedFrom []string                   `json:"derived_from,omitempty"` // source block IDs for derived blocks
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`
}

// Provider preserves the source surface's own type string and IDs verbatim so a
// block can be re-emitted losslessly even when the union does not model a
// concept first-class (union §3).
type Provider struct {
	Vendor     string                     `json:"vendor,omitempty"`  // e.g. anthropic, openai, xai
	Surface    string                     `json:"surface,omitempty"` // e.g. messages, responses, chat_completions
	Model      string                     `json:"model,omitempty"`
	NativeType string                     `json:"native_type,omitempty"` // the provider's own type string
	NativeID   string                     `json:"native_id,omitempty"`   // the provider's own ID
	Ext        map[string]json.RawMessage `json:"ext,omitempty"`
}

// Thread locates a block in the sub-agent / multi-agent tree (union §5A). The
// main thread has ParentThreadID empty. Captures Claude Code sidechains,
// Anthropic Managed-Agents session threads, and Agent Smith sub-agents.
type Thread struct {
	ThreadID       string                     `json:"thread_id,omitempty"`
	ParentBlockID  string                     `json:"parent_block_id,omitempty"`
	ParentThreadID string                     `json:"parent_thread_id,omitempty"`
	AgentID        string                     `json:"agent_id,omitempty"`
	IsSidechain    bool                       `json:"is_sidechain,omitempty"`
	Ext            map[string]json.RawMessage `json:"ext,omitempty"`
}

// Attribution records what produced a block (union §5A) — feeding living-skills
// (AS-049) and /insights cost-by-skill (AS-045).
type Attribution struct {
	Skill     string                     `json:"skill,omitempty"`
	MCPServer string                     `json:"mcp_server,omitempty"`
	MCPTool   string                     `json:"mcp_tool,omitempty"`
	Tool      string                     `json:"tool,omitempty"`
	Hook      string                     `json:"hook,omitempty"`
	Ext       map[string]json.RawMessage `json:"ext,omitempty"`
}

// Tokens is the union of every surveyed provider's usage breakdown (union §8).
// Every field is a pointer: a nil field means "not reported by this surface",
// never zero. Filled later by accounting (AS-020).
type Tokens struct {
	Input        *int                       `json:"input,omitempty"`
	Output       *int                       `json:"output,omitempty"`
	CacheRead    *int                       `json:"cache_read,omitempty"`
	CacheWrite   *int                       `json:"cache_write,omitempty"`
	Reasoning    *int                       `json:"reasoning,omitempty"`
	CacheWrite5m *int                       `json:"cache_write_5m,omitempty"` // Claude ephemeral_5m write count
	CacheWrite1h *int                       `json:"cache_write_1h,omitempty"` // Claude ephemeral_1h write count
	Iterations   []Tokens                   `json:"iterations,omitempty"`     // per-inference-iteration usage within one turn
	Ext          map[string]json.RawMessage `json:"ext,omitempty"`
}

// UsageMeta carries price-affecting usage metadata from the persisted layer
// (union §8).
type UsageMeta struct {
	ServiceTier   string                     `json:"service_tier,omitempty"`
	Speed         string                     `json:"speed,omitempty"`
	ServerToolUse json.RawMessage            `json:"server_tool_use,omitempty"` // billable server-tool call counts
	Ext           map[string]json.RawMessage `json:"ext,omitempty"`
}

// Cache records prompt-caching semantics so the cache-aware assembler (AS-011)
// can place explicit breakpoints (Anthropic) or simply observe automatic
// caching (OpenAI/Grok) under one schema (union §9). The breakpoint marker is
// provenance, not content, so it never mutates a block.
type Cache struct {
	Mode        string                     `json:"mode,omitempty"` // explicit|automatic
	Breakpoints []CacheBreakpoint          `json:"breakpoints,omitempty"`
	TTL         string                     `json:"ttl,omitempty"`
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`
}

// CacheBreakpoint marks a cache breakpoint at a block (union §9).
type CacheBreakpoint struct {
	BlockID string `json:"block_id,omitempty"`
	TTL     string `json:"ttl,omitempty"`
}
