package tidy

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/schema"
)

// Dead-end kinds label why a span is heuristic waste, shown in the fidelity diff
// so the collapse is auditable rather than a silent removal (§9, D0).
const (
	// KindFailedCommand marks a shell command that failed repeatedly — the earlier
	// identical failures are pure flailing the latest attempt already represents.
	KindFailedCommand = "failed-command"
	// KindAbandonedRead marks a file read whose path is never referenced again
	// later in the live window — the content was pulled in and then dropped.
	KindAbandonedRead = "abandoned-read"
)

// Built-in tool names the dead-end signals read: the shell command that flails
// and the searches that show the thread still hunting an abandoned read.
const (
	shellTool = "shell"
	grepTool  = "grep"
	globTool  = "glob"
)

// DeadEnd is one heuristic dead-end span the collapse offers to drop. Like a
// dedup Group it removes only redundant blocks through the same reversible
// exclusion event — the preview decides, no autonomous deletion (AS-043 clarified
// decision). Tokens/CostUSD are the reclaim: the dropped blocks only.
type DeadEnd struct {
	Kind    string // KindFailedCommand | KindAbandonedRead
	Label   string // the command or file path the span concerns
	Detail  string // one-line human explanation for the diff
	Drop    []Item
	Tokens  int
	CostUSD float64
}

// detectDeadEnds finds heuristic dead ends over the live window: shell commands
// that failed repeatedly (keep the latest failure, drop the earlier identical
// ones) and file reads whose path is never referenced again. It is pure and only
// ever proposes dropping redundant blocks — the fidelity diff is the decision
// point. dropped carries the IDs the dedup half already claims so a block is
// never counted twice. seg prices each block and gates on the recent-read
// caution so a just-pulled file is not collapsed out from under the live thread.
func detectDeadEnds(live []schema.Block, seg map[string]composition.Segment, dropped map[string]bool) []DeadEnd {
	out := detectRepeatedFailures(live, seg, dropped)
	out = append(out, detectAbandonedReads(live, seg, dropped)...)
	// Largest reclaim first: the diff leads with the span that frees the most.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	return out
}

// detectRepeatedFailures groups shell commands that failed more than once by
// their normalized command and collapses each group to its latest failure,
// dropping the earlier identical failed call+result pairs. A single failure is
// left alone — one failed attempt is not a dead end, it is a fact the thread may
// still act on.
func detectRepeatedFailures(live []schema.Block, seg map[string]composition.Segment, dropped map[string]bool) []DeadEnd {
	results := indexResults(live)
	type attempt struct {
		call, res schema.Block
		cmd       string
	}
	order := []string{}
	byCmd := map[string][]attempt{}
	for _, b := range live {
		if b.Kind != schema.KindToolCall || b.ToolCall == nil || b.ToolCall.Name != shellTool {
			continue
		}
		cmd := shellCommand(b.ToolCall)
		if cmd == "" {
			continue
		}
		res, ok := results[b.ToolCall.ToolUseID]
		if !ok || !resultFailed(res) {
			continue
		}
		key := normalizeCmd(cmd)
		if _, seen := byCmd[key]; !seen {
			order = append(order, key)
		}
		byCmd[key] = append(byCmd[key], attempt{call: b, res: res, cmd: cmd})
	}

	var out []DeadEnd
	for _, key := range order {
		as := byCmd[key]
		if len(as) < 2 {
			continue // not repeated: no dead end
		}
		de := DeadEnd{
			Kind:   KindFailedCommand,
			Label:  as[0].cmd,
			Detail: "failed repeatedly — earlier identical attempts add nothing the latest failure does not",
		}
		// Keep the latest failing attempt; drop the earlier ones (call + result).
		for _, a := range as[:len(as)-1] {
			addDrop(&de, a.call, seg, dropped)
			addDrop(&de, a.res, seg, dropped)
		}
		if len(de.Drop) > 0 {
			out = append(out, de)
		}
	}
	return out
}

// detectAbandonedReads finds file reads the thread pulled in and then dropped
// while still flailing: the file was read, yet a *later* search (grep/glob) is
// still hunting the same file by name and the exact path is never referenced
// again. Requiring the later search is the precision bar — an ordinary read the
// thread simply keeps in context (never searched for again) is not a dead end,
// only a read the agent visibly moved on from is. A recent read is left alone
// (the live thread may still be working with it), mirroring the dedup recency
// caution.
func detectAbandonedReads(live []schema.Block, seg map[string]composition.Segment, dropped map[string]bool) []DeadEnd {
	texts := make([]string, len(live))
	for i, b := range live {
		texts[i] = blockText(b)
	}
	var out []DeadEnd
	for i, b := range live {
		if b.Kind != schema.KindFileRead || b.FileRead == nil {
			continue
		}
		path := b.FileRead.Path
		if path == "" || dropped[b.ID] {
			continue
		}
		s, ok := seg[b.ID]
		if !ok || s.Age < recentAge {
			continue // not in the window, or too fresh to collapse safely
		}
		base := filepath.Base(path)
		stillHunting, referenced := false, false
		for j := i + 1; j < len(live); j++ {
			if p := searchPattern(live[j]); p != "" && strings.Contains(p, base) {
				stillHunting = true
			}
			if strings.Contains(texts[j], path) {
				referenced = true // a later read/command uses the exact path — still in use
				break
			}
		}
		if !stillHunting || referenced {
			continue
		}
		de := DeadEnd{
			Kind:   KindAbandonedRead,
			Label:  path,
			Detail: "read, then searched for again — the thread moved on without using it",
		}
		addDrop(&de, b, seg, dropped)
		if len(de.Drop) > 0 {
			out = append(out, de)
		}
	}
	return out
}

// searchPattern returns a grep/glob tool call's pattern, else "" for any other
// block — the later-search evidence the abandoned-read signal keys on.
func searchPattern(b schema.Block) string {
	if b.Kind != schema.KindToolCall || b.ToolCall == nil {
		return ""
	}
	if b.ToolCall.Name != grepTool && b.ToolCall.Name != globTool {
		return ""
	}
	var a struct {
		Pattern string `json:"pattern"`
	}
	if len(b.ToolCall.Arguments) > 0 {
		_ = json.Unmarshal(b.ToolCall.Arguments, &a)
	}
	return strings.TrimSpace(a.Pattern)
}

// addDrop appends a block to a dead end's drop list, pricing it from its live
// segment. A block with no live segment, or one the dedup half already drops, is
// skipped so the reclaim is never double-counted.
func addDrop(de *DeadEnd, b schema.Block, seg map[string]composition.Segment, dropped map[string]bool) {
	if dropped[b.ID] {
		return
	}
	s, ok := seg[b.ID]
	if !ok {
		return
	}
	dropped[b.ID] = true // claim it so a later span cannot drop it again
	de.Drop = append(de.Drop, itemOf(s))
	de.Tokens += s.Tokens
	de.CostUSD += s.CostUSD
}

// indexResults maps tool_result bodies (with their owning block) by tool_use id
// so a call can be paired to its outcome.
func indexResults(live []schema.Block) map[string]schema.Block {
	out := map[string]schema.Block{}
	for _, b := range live {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil {
			out[b.ToolResult.ToolUseID] = b
		}
	}
	return out
}

// resultFailed reports whether a tool_result block indicates failure: an explicit
// error flag or a non-zero exit code (nil exit code is unreported, not success).
func resultFailed(b schema.Block) bool {
	r := b.ToolResult
	if r == nil {
		return false
	}
	if r.IsError {
		return true
	}
	return r.ExitCode != nil && *r.ExitCode != 0
}

// shellCommand extracts the command string from a shell tool call's arguments.
func shellCommand(c *schema.ToolCallBody) string {
	var a struct {
		Command string `json:"command"`
	}
	if len(c.Arguments) > 0 {
		_ = json.Unmarshal(c.Arguments, &a)
	}
	return strings.TrimSpace(a.Command)
}

// normalizeCmd collapses internal whitespace so two runs of the same command
// group together regardless of incidental spacing.
func normalizeCmd(cmd string) string {
	return strings.Join(strings.Fields(cmd), " ")
}

// blockText is the searchable serialization of a block used to decide whether a
// read's path is referenced later. Marshaling the whole block captures every
// field a reference could hide in (a later read's path, a tool call's arguments,
// a result's output, assistant text) without enumerating them.
func blockText(b schema.Block) string {
	out, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	return string(out)
}
