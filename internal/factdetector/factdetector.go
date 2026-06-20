// Package factdetector implements the rediscovered-fact detector — the first
// form of living skills (AS-048, PRD §7.20, Decision Log D7). It is a scalpel,
// not a courtroom: it spots trial-and-error that lands on a concrete durable
// fact (a command/path/config value) and offers to save it to the relevant
// skill or memory file, with high precision and a user-checkable trace.
//
// It is a passive system sub-agent (AS-044): observe never runs, teardown reads
// the session's block slice and proposes facts — no model calls, so it is within
// budget when enabled (Decision Log D7: budget/contract grading is demoted to the
// later analyzer, AS-049) and literally free when disabled. Proposals are
// propose-only (D9, C.5): a finding carries a one-line memory/skill diff for the
// user to confirm; the detector never writes.
//
// V1 detects the failed-then-successful command pattern (the canonical "flailing
// to find the test command" case). Repeated-search→path convergence and
// config-key facts are tracked as the precision-tuned follow-on AS-106; shipping
// one mechanical signal well serves D7's high-precision-over-recall mandate
// better than three noisy ones.
//
// Layering: this package consumes subagent + schema and points inward, the same
// way the analyzers sit below the loop (see docs/architecture/package-contracts.md).
package factdetector

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// Name is the built-in sub-agent's stable registry name.
const Name = "rediscovered-fact-detector"

// FindingKind is the kind every proposal this detector emits is tagged with, so
// /insights (AS-045) can recognize a rediscovered-fact offer without running it.
const FindingKind = "rediscovered_fact"

// CommandFact marks a durable command fact (the only fact kind in V1).
const CommandFact = "command"

// DefaultTarget is the fallback save target — the project-root memory file —
// used when no resolver is wired or the resolver declines (the C.1 save-target
// resolution's last fallback).
const DefaultTarget = "AGENT.md"

// shellTool is the built-in shell tool's name; its arguments carry the command.
const shellTool = "shell"

// Resolve picks where a discovered fact should be saved (the C.1 save-target
// rule): prefer the active skill's memory/contract when the trace is inside a
// skill scope (skill non-empty), otherwise the deepest applicable memory file
// for the files involved, falling back to the project-root memory file.
// Returning "" means "not resolved"; the detector then uses DefaultTarget. A nil
// Resolve always uses DefaultTarget — the memory/skill-backed resolver is wired
// by the consumer (AS-088), keeping this package free of those imports.
type Resolve func(skill string, files []string) string

// Outcome is how the user responded to an offered fact, recorded in the Ledger
// to track the precision bar (accepted vs dismissed) and to suppress re-offering
// a dismissed fact.
type Outcome string

const (
	// Accepted means the user saved the fact (writes the diff via preview).
	Accepted Outcome = "accepted"
	// Dismissed means the user declined it; the same fact is not re-suggested.
	Dismissed Outcome = "dismissed"
)

// Stats is the precision tally: how many offered facts were accepted vs
// dismissed. Repeated low acceptance is a detector-quality bug (the ticket's
// precision bar), surfaced rather than chased to an exact gate before volume.
type Stats struct {
	Accepted  int
	Dismissed int
}

// Total is the number of resolved offers (accepted + dismissed).
func (s Stats) Total() int { return s.Accepted + s.Dismissed }

// Precision is the accepted fraction of resolved offers, 0 when none resolved.
func (s Stats) Precision() float64 {
	if s.Total() == 0 {
		return 0
	}
	return float64(s.Accepted) / float64(s.Total())
}

// Ledger persists user responses to offered facts across sessions: it suppresses
// re-offering a dismissed fact and tracks the precision tally. It is an interface
// so a durable backing (the cross-session rollup, AS-050) can replace the
// in-memory default later; V1 ships MemLedger.
type Ledger interface {
	// Dismissed reports whether the fact with this fingerprint was already
	// declined and so must not be offered again.
	Dismissed(fingerprint string) bool
	// Record files the user's response to an offered fact.
	Record(fingerprint string, o Outcome)
	// Stats returns the running precision tally.
	Stats() Stats
}

// MemLedger is an in-memory Ledger: the default within a process. It is safe for
// concurrent use so teardown (which reads it) and the offer UI (which records)
// can run on different goroutines.
type MemLedger struct {
	mu        sync.Mutex
	dismissed map[string]bool
	stats     Stats
}

// NewMemLedger returns an empty in-memory Ledger.
func NewMemLedger() *MemLedger { return &MemLedger{dismissed: map[string]bool{}} }

// Dismissed reports whether fingerprint was previously declined.
func (l *MemLedger) Dismissed(fingerprint string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dismissed[fingerprint]
}

// Record files an outcome, updating the dismissal set and the precision tally.
func (l *MemLedger) Record(fingerprint string, o Outcome) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch o {
	case Accepted:
		l.stats.Accepted++
	case Dismissed:
		l.stats.Dismissed++
		l.dismissed[fingerprint] = true
	}
}

// Stats returns a snapshot of the precision tally.
func (l *MemLedger) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}

// Detector is the rediscovered-fact sub-agent. It is stateless across the
// lifecycle (analysis happens in Teardown over the handed slice), so a single
// instance is reusable; Factory yields fresh ones anyway, per the framework's
// per-session instancing rule.
type Detector struct {
	resolve Resolve
	ledger  Ledger
}

// New builds a Detector. A nil resolve falls back to DefaultTarget for every
// fact; a nil ledger means no dismissals are suppressed and no precision is
// tracked (offers are always made). The consumer wires both (AS-088).
func New(resolve Resolve, ledger Ledger) *Detector {
	return &Detector{resolve: resolve, ledger: ledger}
}

// Factory returns a subagent.Factory that builds Detectors sharing one resolve
// and ledger, so dismissals and precision accumulate across the process's
// sessions while each session still gets its own (stateless) instance.
func Factory(resolve Resolve, ledger Ledger) subagent.Factory {
	return func() subagent.SubAgent { return New(resolve, ledger) }
}

// Manifest declares the detector: a passive analyzer that tears down once at
// session end over the whole session, defaults on (it costs nothing — no model
// tier), proposes edits, and reads the transcript.
func (d *Detector) Manifest() subagent.Manifest {
	return subagent.Manifest{
		Name:             Name,
		Kind:             subagent.KindAnalyzer,
		Schedule:         subagent.AtSessionEnd,
		Scope:            subagent.SessionScope,
		EnabledByDefault: true,
		ModelTier:        "", // no model use in V1 → zero cost even when enabled
		BudgetUSD:        0,
		Emits:            []string{FindingKind},
		Permissions:      []subagent.Permission{subagent.ReadTranscript, subagent.ProposeEdit},
	}
}

// Init is a no-op: there is no per-scope state to set up.
func (d *Detector) Init(subagent.Scope) {}

// Observe is a no-op: the detector analyzes the slice handed to Teardown rather
// than accumulating per block, so it adds no per-block work to a turn.
func (d *Detector) Observe(schema.Block) {}

// Teardown scans the session's blocks for rediscovered facts and returns one
// propose-only finding per fact, skipping any the user previously dismissed. It
// spends nothing (no model calls), so it is never budget-capped.
func (d *Detector) Teardown(_ subagent.Scope, slice []schema.Block) subagent.Result {
	var findings []subagent.Finding
	seen := map[string]bool{} // de-dupe within one session
	for _, c := range detectCommands(slice) {
		if seen[c.Fingerprint] {
			continue
		}
		seen[c.Fingerprint] = true
		if d.ledger != nil && d.ledger.Dismissed(c.Fingerprint) {
			continue // declined before: do not re-suggest the same fact
		}
		findings = append(findings, d.finding(c))
	}
	return subagent.Result{Findings: findings}
}

// finding turns a candidate into a propose-only Finding with a one-line diff to
// the resolved save target.
func (d *Detector) finding(c candidate) subagent.Finding {
	target := DefaultTarget
	if d.resolve != nil {
		if t := d.resolve(c.Skill, nil); t != "" {
			target = t
		}
	}
	return subagent.Finding{
		Kind:    FindingKind,
		Summary: fmt.Sprintf("Rediscovered working command: %s", c.Value),
		Detail:  c.evidence(),
		Proposal: &subagent.Edit{
			Target:      target,
			Description: c.diff(),
		},
	}
}

// candidate is one durable fact found through trial and error, with the trace
// that justifies it (the user-checkable evidence D7 requires).
type candidate struct {
	Kind        string
	Value       string   // the durable fact (the working command)
	Fingerprint string   // stable identity for de-dupe and dismissal
	Skill       string   // the active skill when discovered, if any
	Failed      []string // the failed variants tried first
}

// diff is the minimal one-line memory/skill addition proposed for this fact.
func (c candidate) diff() string {
	return fmt.Sprintf("+ `%s` — working command (found after trial and error)", c.Value)
}

// evidence is the human-readable trace backing the proposal.
func (c candidate) evidence() string {
	if len(c.Failed) == 0 {
		return ""
	}
	tried := make([]string, len(c.Failed))
	for i, f := range c.Failed {
		tried[i] = "`" + f + "`"
	}
	return "tried " + strings.Join(tried, ", ") + " before `" + c.Value + "` worked"
}

// commandFingerprint is the stable identity of a command fact.
func commandFingerprint(cmd string) string {
	return CommandFact + ":" + normalize(cmd)
}

// detectCommands finds the failed-then-successful command pattern: one or more
// failed shell commands followed by a successful one that shares a significant
// token with a failure — the success is the durable fact. A success ends the
// current flail run, so unrelated earlier failures do not cling to a later
// success. High precision: a candidate needs both prior flailing and a
// meaningful token link to it.
func detectCommands(slice []schema.Block) []candidate {
	results := pairResults(slice)
	var out []candidate
	var failed []shellExec // the current flail run
	for _, e := range execs(slice, results) {
		if e.failed {
			failed = append(failed, e)
			continue
		}
		// A success: did it resolve recent flailing on the same thing? Detection
		// requires at least one failure sharing a significant token (precision),
		// but once that link is established the whole flail run is the trace
		// (richer, user-checkable evidence).
		if related(e, failed) {
			out = append(out, candidate{
				Kind:        CommandFact,
				Value:       e.command,
				Fingerprint: commandFingerprint(e.command),
				Skill:       e.skill,
				Failed:      commands(failed),
			})
		}
		failed = nil // a success always ends the flail run
	}
	return out
}

// related reports whether any failed exec shares a significant token with the
// successful one (so the success plausibly resolves that flailing).
func related(success shellExec, failed []shellExec) bool {
	want := tokens(success.command)
	for _, f := range failed {
		if shares(want, tokens(f.command)) {
			return true
		}
	}
	return false
}

// shellExec is one shell command execution paired with its outcome.
type shellExec struct {
	command string
	failed  bool
	skill   string
}

// commands extracts the command strings from a list of execs.
func commands(es []shellExec) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.command
	}
	return out
}

// execs walks the slice in order and yields each shell command execution with
// its outcome, pairing a tool_call to its tool_result by tool_use id.
func execs(slice []schema.Block, results map[string]*schema.ToolResultBody) []shellExec {
	var out []shellExec
	for _, b := range slice {
		if b.Kind != schema.KindToolCall || b.ToolCall == nil || b.ToolCall.Name != shellTool {
			continue
		}
		cmd := command(b.ToolCall)
		if cmd == "" {
			continue
		}
		res, ok := results[b.ToolCall.ToolUseID]
		if !ok {
			continue // no paired result: outcome unknown, skip
		}
		out = append(out, shellExec{
			command: cmd,
			failed:  isFailure(res),
			skill:   skillOf(b),
		})
	}
	return out
}

// pairResults indexes tool_result bodies by their tool_use id.
func pairResults(slice []schema.Block) map[string]*schema.ToolResultBody {
	out := map[string]*schema.ToolResultBody{}
	for i := range slice {
		b := slice[i]
		if b.Kind == schema.KindToolResult && b.ToolResult != nil {
			out[b.ToolResult.ToolUseID] = b.ToolResult
		}
	}
	return out
}

// isFailure reports whether a tool result indicates the command failed: an
// explicit error flag, or a non-zero exit code (nil exit code is unreported, not
// success — but then IsError carries the signal).
func isFailure(r *schema.ToolResultBody) bool {
	if r.IsError {
		return true
	}
	return r.ExitCode != nil && *r.ExitCode != 0
}

// command extracts the shell command string from a tool call's arguments.
func command(c *schema.ToolCallBody) string {
	var args struct {
		Command string `json:"command"`
	}
	if len(c.Arguments) > 0 {
		_ = json.Unmarshal(c.Arguments, &args)
	}
	return strings.TrimSpace(args.Command)
}

// skillOf returns the skill that produced a block, if attributed.
func skillOf(b schema.Block) string {
	if b.Attribution != nil {
		return b.Attribution.Skill
	}
	return ""
}

// normalize collapses internal whitespace and trims a command for fingerprinting.
func normalize(cmd string) string {
	return strings.Join(strings.Fields(cmd), " ")
}

// tokens returns the significant tokens of a command: alphabetic-led words of
// length >= 2 (program names and subcommands like "make", "test", "migrate"),
// lowercased. Flags ("-v"), paths ("./..."), and redirections ("2>&1") are
// excluded by the alphabetic-led rule, so a shared token is a meaningful link,
// not shell noise.
func tokens(cmd string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(cmd)) {
		if len(w) < 2 {
			continue
		}
		if c := w[0]; c < 'a' || c > 'z' {
			continue
		}
		out[w] = true
	}
	return out
}

// shares reports whether two token sets intersect.
func shares(a, b map[string]bool) bool {
	for t := range a {
		if b[t] {
			return true
		}
	}
	return false
}
