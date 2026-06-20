// Package topic derives deterministic topic/tag labels for projected blocks
// (AS-027). §5 of the PRD says every segment knows "its topic/tags"; that powers
// the topic dimension of /context (AS-026) and is the first-pass candidate set
// semantic /clean (AS-029) matches against.
//
// V1 labels are deterministic heuristics only — file paths/modules, tool names,
// command (producer) names, skill/MCP attribution, and block kind/role. There
// are no embeddings and no model calls here, so labeling has zero provider-token
// cost and never blocks an interactive turn (a Tags call is pure, given a
// block). Labels are derived additively from a block and never mutate it (D3):
// callers compute Tags on demand, the block is untouched.
package topic

import (
	"path"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/schema"
)

// Tags returns the deterministic topic tags for b, sorted and de-duplicated.
// The result is never empty: every block gets at least one coarse type tag
// (e.g. "file", "tool", "conversation"), plus any discovered file/module, tool,
// command, skill, or MCP tags. The same block always yields the same tags, so
// the set is a stable handle AS-029 can match against.
func Tags(b schema.Block) []string {
	set := map[string]struct{}{}
	add := func(t string) {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = struct{}{}
		}
	}

	// Coarse type tag — always present, so every segment carries a topic.
	add(coarseType(b))

	// File / module tag from a read path: the containing directory groups reads
	// by module, the granularity /context and AS-029 navigate by.
	if b.FileRead != nil && b.FileRead.Path != "" {
		add("file:" + moduleDir(b.FileRead.Path))
	}
	// The specific tool, beyond the coarse "tool" bucket.
	if b.ToolCall != nil && b.ToolCall.Name != "" {
		add("tool:" + b.ToolCall.Name)
	}
	// Attribution: what produced the block (AS-034 skills, AS-036 MCP, AS-035
	// hooks, tools). MCP tags both the server and the specific tool so /clean
	// (AS-029) can match either granularity.
	if a := b.Attribution; a != nil {
		add(prefixed("tool:", a.Tool))
		add(prefixed("skill:", a.Skill))
		if a.MCPServer != "" {
			add("mcp:" + a.MCPServer)
			if a.MCPTool != "" {
				add("mcp:" + a.MCPServer + "/" + a.MCPTool)
			}
		}
		add(prefixed("hook:", a.Hook))
	}
	// The command (producer) that appended the block — e.g. "/goal", "/clean".
	if b.Provenance != nil {
		add(prefixed("cmd:", b.Provenance.Producer))
	}

	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Primary returns the lead tag for b — the first specific tag (one carrying a
// ":", e.g. "file:internal/topic" or "tool:shell"), falling back to the
// lexically first (coarse) tag when there is none. Preferring the specific tag
// keeps the "by topic" sort distinct from the "by type" sort, which already
// groups on the coarse tag. It is never empty.
func Primary(b schema.Block) string {
	tags := Tags(b)
	for _, t := range tags {
		if strings.Contains(t, ":") {
			return t
		}
	}
	return tags[0]
}

// coarseType buckets a block by kind, then role, into a single coarse tag. Every
// block maps to exactly one, so Tags is never empty.
func coarseType(b schema.Block) string {
	switch b.Kind {
	case schema.KindFileRead:
		return "file"
	case schema.KindToolCall, schema.KindToolResult:
		return "tool"
	case schema.KindReasoning:
		return "reasoning"
	case schema.KindCompaction:
		return "compaction"
	case schema.KindFallback:
		return "fallback"
	}
	switch b.Role {
	case schema.RoleSystem, schema.RoleHarness:
		return "system"
	default: // user, assistant, tool, or unset
		return "conversation"
	}
}

// moduleDir reduces a read path to its containing directory (slash-normalized),
// the module-level tag. A top-level file (no directory) is tagged by its own
// name so it still gets a stable, specific handle.
func moduleDir(p string) string {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if d := path.Dir(p); d != "." && d != "/" && d != "" {
		return d
	}
	return path.Base(p)
}

// prefixed returns prefix+v when v is non-empty, else "" so add skips it.
func prefixed(prefix, v string) string {
	if v == "" {
		return ""
	}
	return prefix + v
}
