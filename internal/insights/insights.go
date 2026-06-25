// Package insights is Agent Smith's session retrospective engine (AS-045, PRD
// §7.14, the flagship /insights wedge). It turns a session's append-only block
// log into a dashboard of measured signals — cost per turn, the costliest turns,
// repeated file reads and commands, oversized tool outputs, error loops, and the
// live-vs-stale context-health ratio — and derives grounded, applicable
// suggestions from them.
//
// Measured-first (§9 mitigation): every signal and every suggestion is computed
// from the log and cites its evidence (turn/block references), never vibes. The
// dashboard renders with zero model calls, so it is free and always available —
// the "model layer disabled" zero-cost mode is the default here. The deterministic
// suggestions below already satisfy the AC of ≥1 specific, applicable suggestion,
// and a deterministic goal assessment (AS-040/AS-109) answers "was the objective
// met?" from the log alone.
//
// The same analysis powers the insights-writer system sub-agent (writer.go),
// which records the suggestions as findings at session end (AS-044/AS-107), so
// the cross-session rollup (AS-050/AS-057) reads the same data the panel shows.
// When the AS-109 model-assisted layer is enabled, the writer additionally calls
// the Proposer seam (model.go) for richer, model-authored suggestions — each still
// grounded in measured evidence — implemented out-of-package by internal/insightsmodel.
//
// Layering: this package consumes cost, projection, render, and schema and points
// inward, the same way the other analyzers sit below the loop (see
// docs/architecture/package-contracts.md).
package insights

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// shellTool is the built-in shell tool whose arguments carry the command string,
// matched the same way the rediscovered-fact detector matches it (factdetector).
const shellTool = "shell"

// goalProducer / goalPrefix mirror internal/goal by value (the same value-coupling
// pattern shellTool uses for the shell tool) so the retro can recognize the active
// /goal block without importing the goal command package — keeping insights
// pointing inward (cost, projection, render, schema only). They MUST stay in sync
// with goal.Producer / goal.textPrefix.
const (
	goalProducer = "/goal"
	goalPrefix   = "Session goal: "
)

// Thresholds for surfacing a signal. They are deliberate, high-precision bars
// (PRD §7.14: suggestions must be specific and applicable, not noisy): a command
// run this often is a workflow fact worth memorializing; a single tool output
// this large is the "40k unused tokens" case worth scoping.
const (
	repeatedCommandMin = 3    // a command run this many times is a workflow fact
	repeatedReadMin    = 2    // re-reading a file is wasted work worth flagging
	bigOutputTokens    = 2000 // a single tool output this large is worth scoping
	errorLoopMin       = 3    // this many failed tool calls is a retry loop
	topN               = 3    // how many entries each ranked section shows
)

// trivialCommands are shell commands too generic to memorialize: flagging "ran
// `ls` 5×" as a durable fact would be noise, so they never seed a suggestion.
var trivialCommands = map[string]bool{
	"ls": true, "pwd": true, "cd": true, "cat": true, "echo": true,
	"clear": true, "which": true, "env": true, "true": true,
}

// Report is the measured retrospective for one session: the dashboard /insights
// renders and the data the insights-writer reports. Every field is derived from
// the log, so a Report reproduces exactly from a saved session.
type Report struct {
	Turns       int     // priced turns (usage events)
	TotalTokens int     // billable tokens across the session
	TotalUSD    float64 // session dollar total (lower bound when AllPriced is false)
	AllPriced   bool
	Currency    string

	Costliest     []cost.TurnCost // the most expensive turns, costliest first
	RepeatedReads []Repeat        // files read more than once (wasted work)
	RepeatedCmds  []Repeat        // commands run repeatedly (memory-file candidates)
	BigOutputs    []Output        // oversized tool outputs (scope/clean candidates)
	Errors        int             // failed tool calls (retry-loop signal)
	LiveBlocks    int             // blocks still in the model-facing window
	StaleBlocks   int             // blocks dropped from the window (excluded/replay)

	Goal        *GoalAssessment // the session objective (AS-040) and whether it was met; nil when no /goal was set
	Suggestions []Suggestion    // grounded, ordered; apply by 1-based index
}

// GoalAssessment answers "did the session meet its objective?" (AS-109 goal
// anchoring, PRD §7.14) for the active /goal (AS-040), grounded in measured
// signals only. Completion is read from the log: a goal retired via `/goal done`
// (no longer live, with no successor goal) is treated as met; a still-live goal is
// in progress. The model-assisted layer may add reasoning on top, but the verdict
// itself is measured, never vibes — so it renders identically with the model layer
// disabled.
type GoalAssessment struct {
	Objective string // the goal text (prefix stripped)
	Met       bool   // true when the objective was marked complete (`/goal done`)
	Status    string // "completed" | "in-progress"
	Evidence  string // the measured grounding (goal anchor + turn/error counts)
	Seq       int    // the goal block's sequence number, for the jump-to-transcript link
}

// Repeat is one repeated value (a file path or a command) with its occurrence
// count and the block sequence numbers where it appeared — the jump-to-transcript
// anchors a face can scroll to (PRD §7.14: cite measured evidence with a link).
type Repeat struct {
	Value string
	Count int
	Seqs  []int
}

// Output is one tool result and the window tokens it occupies, with the block
// sequence number for the jump-to link.
type Output struct {
	Tool   string
	Tokens int
	Seq    int
}

// Suggestion is one grounded, actionable recommendation. Evidence cites the
// measured signal (counts plus block anchors); a non-nil Edit makes it a
// one-click memory-file change (applied via /insights apply), and a nil Edit is
// copyable guidance only (PRD §7.14: apply where safe, copy otherwise).
type Suggestion struct {
	Summary  string
	Evidence string
	Edit     *MemoryEdit
	// Source distinguishes a measured suggestion (the default, "") from a
	// model-authored one ("model") added by the AS-109 model-assisted layer. A
	// model suggestion still cites measured evidence (the §9 non-negotiable); the
	// label only marks provenance so a face can flag the richer prose.
	Source string
}

// SourceModel tags a Suggestion authored by the model-assisted layer (AS-109).
const SourceModel = "model"

// MemoryEdit is a propose-only line to append to a memory file. Target is the
// memory file (the project-root memory file by default); Line is the addition,
// rendered as a `+` diff before it lands.
type MemoryEdit struct {
	Target string
	Line   string
}

// DefaultMemoryTarget is the fallback memory file a suggestion proposes when no
// more specific target is resolved — the project-root memory file, matching the
// rediscovered-fact detector's default (factdetector.DefaultTarget).
const DefaultMemoryTarget = "AGENT.md"

// Analyze builds the retrospective for a session's events, pricing turns against
// table at model. A nil table still yields exact token counts and every
// non-cost signal and suggestion (the insights-writer runs this way), so the
// dashboard never depends on pricing being configured.
func Analyze(events []schema.Block, table *cost.Table, model string) Report {
	sum := cost.Summarize(events, table)
	r := Report{
		Turns:       len(sum.Turns),
		TotalTokens: sum.Total.Total(),
		TotalUSD:    sum.TotalUSD,
		AllPriced:   sum.AllPriced,
		Currency:    sum.Currency,
		Costliest:   costliest(sum.Turns),
	}
	r.RepeatedReads = repeatedReads(events)
	r.RepeatedCmds = repeatedCommands(events)
	r.BigOutputs = bigOutputs(events)
	r.Errors = errorCount(events)

	proj := projection.Project(events, projection.Options{TargetModel: model})
	r.LiveBlocks = len(proj.Live())
	r.StaleBlocks = len(proj.Blocks()) - r.LiveBlocks
	r.Goal = goalAssessment(proj, r)

	r.Suggestions = suggestions(r)
	return r
}

// goalAssessment reads the session's /goal objective (AS-040) from the projection
// and reports whether it was met, grounded only in measured signals. It returns
// nil when no goal was ever set. A goal block still live in the window is in
// progress; a goal that was retired with no live successor was completed via
// `/goal done` — the measured completion signal. Replacing a goal leaves the new
// one live, so only a truly finished objective reads as met.
func goalAssessment(proj *projection.Projection, r Report) *GoalAssessment {
	var latest *GoalAssessment
	live := false
	for _, b := range proj.Blocks() {
		if b.Kind != schema.KindText || b.Text == nil ||
			b.Provenance == nil || b.Provenance.Producer != goalProducer {
			continue
		}
		latest = &GoalAssessment{
			Objective: strings.TrimPrefix(b.Text.Text, goalPrefix),
			Seq:       b.Seq,
		}
		live = b.Live
	}
	if latest == nil {
		return nil
	}
	if live {
		latest.Status = "in-progress"
		latest.Evidence = "goal set at " + anchors([]int{latest.Seq}) + "; " +
			plural(r.Turns, "turn") + ", " + plural(r.Errors, "tool error") + " so far"
	} else {
		latest.Met = true
		latest.Status = "completed"
		latest.Evidence = "goal completed (retired) after " + plural(r.Turns, "turn") +
			"; set at " + anchors([]int{latest.Seq})
	}
	return latest
}

// costliest returns the most expensive turns, costliest first. It ranks by
// dollars when priced and falls back to total tokens so an unpriced session
// still surfaces its heaviest turns; turns with no measurable weight are dropped.
func costliest(turns []cost.TurnCost) []cost.TurnCost {
	ranked := make([]cost.TurnCost, 0, len(turns))
	for _, t := range turns {
		if t.TotalUSD > 0 || t.Tokens.Total() > 0 {
			ranked = append(ranked, t)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].TotalUSD != ranked[j].TotalUSD {
			return ranked[i].TotalUSD > ranked[j].TotalUSD
		}
		return ranked[i].Tokens.Total() > ranked[j].Tokens.Total()
	})
	if len(ranked) > topN {
		ranked = ranked[:topN]
	}
	return ranked
}

// repeatedReads finds files read more than once — re-reading the same file is
// wasted window the user could keep in memory or context.
func repeatedReads(events []schema.Block) []Repeat {
	counts := map[string]*Repeat{}
	var order []string
	for _, b := range events {
		if b.Kind != schema.KindFileRead || b.FileRead == nil || b.FileRead.Path == "" {
			continue
		}
		p := b.FileRead.Path
		if counts[p] == nil {
			counts[p] = &Repeat{Value: p}
			order = append(order, p)
		}
		counts[p].Count++
		counts[p].Seqs = append(counts[p].Seqs, b.Seq)
	}
	return rank(counts, order, repeatedReadMin)
}

// repeatedCommands finds shell commands run at least repeatedCommandMin times,
// skipping trivial navigation so only durable workflow commands ("make test"
// typed 4×) become memory-file candidates.
func repeatedCommands(events []schema.Block) []Repeat {
	counts := map[string]*Repeat{}
	var order []string
	for _, b := range events {
		if b.Kind != schema.KindToolCall || b.ToolCall == nil || b.ToolCall.Name != shellTool {
			continue
		}
		cmd := normalize(shellCommand(b.ToolCall))
		if cmd == "" || trivialCommands[firstWord(cmd)] {
			continue
		}
		if counts[cmd] == nil {
			counts[cmd] = &Repeat{Value: cmd}
			order = append(order, cmd)
		}
		counts[cmd].Count++
		counts[cmd].Seqs = append(counts[cmd].Seqs, b.Seq)
	}
	return rank(counts, order, repeatedCommandMin)
}

// rank filters the counted values to those meeting min and returns them
// most-frequent first, with first-seen order as a stable tie-break.
func rank(counts map[string]*Repeat, order []string, min int) []Repeat {
	out := make([]Repeat, 0, len(order))
	for _, k := range order {
		if counts[k].Count >= min {
			out = append(out, *counts[k])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// bigOutputs finds the largest tool results by estimated window tokens, keeping
// only those over bigOutputTokens — the "this tool returned 40k unused tokens"
// case worth scoping or cleaning.
func bigOutputs(events []schema.Block) []Output {
	names := toolNames(events)
	var out []Output
	for _, b := range events {
		if b.Kind != schema.KindToolResult || b.ToolResult == nil {
			continue
		}
		tokens := cost.EstimateBlockTokens(b)
		if tokens < bigOutputTokens {
			continue
		}
		out = append(out, Output{Tool: names[b.ToolResult.ToolUseID], Tokens: tokens, Seq: b.Seq})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// toolNames indexes tool names by their tool_use id so a result block (which
// carries only the id) can be labelled with the tool that produced it.
func toolNames(events []schema.Block) map[string]string {
	names := map[string]string{}
	for _, b := range events {
		if b.Kind == schema.KindToolCall && b.ToolCall != nil {
			names[b.ToolCall.ToolUseID] = b.ToolCall.Name
		}
	}
	return names
}

// errorCount tallies failed tool calls: an explicit error flag or a non-zero
// exit code. A high count is the retry-loop signal a suggestion surfaces.
func errorCount(events []schema.Block) int {
	n := 0
	for _, b := range events {
		if b.Kind != schema.KindToolResult || b.ToolResult == nil {
			continue
		}
		if b.ToolResult.IsError || (b.ToolResult.ExitCode != nil && *b.ToolResult.ExitCode != 0) {
			n++
		}
	}
	return n
}

// suggestions derives grounded, ordered recommendations from the measured
// signals. Applicable memory-file edits (the repeated-command case) come first so
// the most actionable item leads; the rest are copyable guidance.
func suggestions(r Report) []Suggestion {
	var out []Suggestion
	for _, c := range r.RepeatedCmds {
		out = append(out, Suggestion{
			Summary:  "Add `" + c.Value + "` to " + DefaultMemoryTarget + " — run " + times(c.Count),
			Evidence: "command ran " + times(c.Count) + " at " + anchors(c.Seqs),
			Edit: &MemoryEdit{
				Target: DefaultMemoryTarget,
				Line:   "- `" + c.Value + "` — frequently-used command (run " + times(c.Count) + " this session)",
			},
		})
	}
	for _, o := range r.BigOutputs {
		tool := o.Tool
		if tool == "" {
			tool = "a tool"
		}
		out = append(out, Suggestion{
			Summary:  "Scope `" + tool + "` or /clean its output (~" + plural(o.Tokens, "token") + ")",
			Evidence: "output ~" + plural(o.Tokens, "token") + " at " + anchors([]int{o.Seq}),
		})
	}
	for _, rd := range r.RepeatedReads {
		out = append(out, Suggestion{
			Summary:  "Re-read " + rd.Value + " " + times(rd.Count) + " — keep it in context",
			Evidence: "read " + times(rd.Count) + " at " + anchors(rd.Seqs),
		})
	}
	if r.Errors >= errorLoopMin {
		out = append(out, Suggestion{
			Summary:  "Review the retry loop — " + plural(r.Errors, "tool error"),
			Evidence: plural(r.Errors, "failed tool call") + " this session",
		})
	}
	return out
}

// shellCommand extracts the command string from a shell tool call's arguments.
func shellCommand(c *schema.ToolCallBody) string {
	var args struct {
		Command string `json:"command"`
	}
	if len(c.Arguments) > 0 {
		_ = json.Unmarshal(c.Arguments, &args)
	}
	return strings.TrimSpace(args.Command)
}

// normalize collapses internal whitespace and trims a command so the same
// command typed with different spacing counts once.
func normalize(cmd string) string { return strings.Join(strings.Fields(cmd), " ") }

// firstWord returns the leading token of a command (its program name), used to
// skip trivial navigation commands.
func firstWord(cmd string) string {
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}
