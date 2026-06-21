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
// Three mechanical signals, each held to D7's high-precision-over-recall bar (a
// clear trial-and-error link, never a single lucky call):
//
//   - command (AS-048): a failed-then-successful shell command sharing a
//     significant token (the canonical "flailing to find the test command").
//   - path (AS-106): ≥2 searches (grep/glob) whose pattern names a file that a
//     later successful read settles on — "the thing lives at <path>".
//   - config (AS-106): a shell run that fails naming a missing env var (an
//     allow-list of stderr signatures), then succeeds once it is set.
//
// Layering: this package consumes subagent + schema and points inward, the same
// way the analyzers sit below the loop (see docs/architecture/package-contracts.md).
package factdetector

import (
	"encoding/json"
	"fmt"
	"regexp"
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

// CommandFact marks a durable command fact (the failed-then-successful command).
const CommandFact = "command"

// PathFact marks a durable file-location fact (searches converging on a path).
const PathFact = "path"

// ConfigFact marks a durable config fact (a required env var found the hard way).
const ConfigFact = "config"

// Tool names whose calls the path signal reads: searches that flail and the read
// that settles on a concrete file.
const (
	readToolName = "read"
	grepToolName = "grep"
	globToolName = "glob"
)

// minSearchFlail is how many searches must precede a settling read before the
// path signal fires — D7 precision: a single lucky search is not rediscovery.
const minSearchFlail = 2

// minToken is the shortest alphanumeric token treated as a meaningful link
// between a search pattern and a path (drops "go", "md", and other noise).
const minToken = 3

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

// Dismissed reports whether fingerprint was previously declined. Reading a nil
// map is safe (yields false), so the zero-value MemLedger needs no init here.
func (l *MemLedger) Dismissed(fingerprint string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dismissed[fingerprint]
}

// Record files an outcome, updating the dismissal set and the precision tally.
// The map is lazily initialized so a zero-value MemLedger does not panic.
func (l *MemLedger) Record(fingerprint string, o Outcome) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dismissed == nil {
		l.dismissed = map[string]bool{}
	}
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
	seen := map[string]bool{} // de-dupe within one session (kind-prefixed fingerprints never collide)
	candidates := detectCommands(slice)
	candidates = append(candidates, detectPaths(slice)...)
	candidates = append(candidates, detectConfigKeys(slice)...)
	for _, c := range candidates {
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
		if t := d.resolve(c.Skill, c.files()); t != "" {
			target = t
		}
	}
	return subagent.Finding{
		Kind:    FindingKind,
		Summary: c.summary(),
		Detail:  c.evidence(),
		Proposal: &subagent.Edit{
			Target:      target,
			Description: c.diff(),
		},
	}
}

// candidate is one durable fact found through trial and error, with the trace
// that justifies it (the user-checkable evidence D7 requires). Failed holds the
// prior attempts that justify the fact — failed commands, flailing search
// patterns, or the run that named a missing config key — depending on Kind.
type candidate struct {
	Kind        string
	Value       string   // the durable fact (command, file path, or config key)
	Fingerprint string   // stable identity for de-dupe and dismissal
	Skill       string   // the active skill when discovered, if any
	Failed      []string // the prior attempts that justify the fact
}

// summary is the human-readable headline for the offered fact.
func (c candidate) summary() string {
	switch c.Kind {
	case PathFact:
		return fmt.Sprintf("Rediscovered file location: %s", c.Value)
	case ConfigFact:
		return fmt.Sprintf("Rediscovered required config: %s", c.Value)
	default:
		return fmt.Sprintf("Rediscovered working command: %s", c.Value)
	}
}

// diff is the minimal one-line memory/skill addition proposed for this fact.
func (c candidate) diff() string {
	switch c.Kind {
	case PathFact:
		return fmt.Sprintf("+ `%s` — where the relevant code lives (found after searching)", c.Value)
	case ConfigFact:
		return fmt.Sprintf("+ `%s` — required config/env (discovered through a failed run)", c.Value)
	default:
		return fmt.Sprintf("+ `%s` — working command (found after trial and error)", c.Value)
	}
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
	switch c.Kind {
	case PathFact:
		return "searched " + strings.Join(tried, ", ") + " before `" + c.Value + "` had it"
	case ConfigFact:
		return "`" + c.Value + "` was missing when " + strings.Join(tried, ", ") + " failed"
	default:
		return "tried " + strings.Join(tried, ", ") + " before `" + c.Value + "` worked"
	}
}

// files is the set of files the fact concerns, so the save-target resolver can
// pick the deepest applicable memory file. Only a path fact names a file.
func (c candidate) files() []string {
	if c.Kind == PathFact && c.Value != "" {
		return []string{c.Value}
	}
	return nil
}

// commandFingerprint is the stable identity of a command fact.
func commandFingerprint(cmd string) string {
	return CommandFact + ":" + normalize(cmd)
}

// pathFingerprint is the stable identity of a file-location fact.
func pathFingerprint(p string) string {
	return PathFact + ":" + normalize(p)
}

// configFingerprint is the stable identity of a config-key fact.
func configFingerprint(key string) string {
	return ConfigFact + ":" + normalize(key)
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
		// (richer, user-checkable evidence). Only a *related* success clears the
		// run: an unrelated success (an informational `ls`/`pwd`/`git status`
		// mid-flail) is ignored so it cannot orphan the failures from the command
		// that actually resolves them.
		if related(e, failed) {
			out = append(out, candidate{
				Kind:        CommandFact,
				Value:       e.command,
				Fingerprint: commandFingerprint(e.command),
				Skill:       e.skill,
				Failed:      commands(failed),
			})
			failed = nil
		}
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
		// Trim a leading path prefix so a local script (`./test.sh`,
		// `/usr/bin/python`) tokenizes on its name rather than being dropped for
		// starting with a non-letter.
		w = strings.TrimLeft(w, "./\\")
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

// detectPaths finds the search→path-convergence pattern: at least minSearchFlail
// grep/glob searches whose pattern names a file, resolved by a later successful
// read settling on that path. Precision (D7): a token of the read path (basename
// or a path segment) must appear in a preceding search pattern, and a single
// direct read with no flailing is never flagged. Any successful read ends the
// current flail run, so an unrelated read cannot orphan earlier searches.
func detectPaths(slice []schema.Block) []candidate {
	results := pairResults(slice)
	var out []candidate
	var searches []string // patterns of the current flail run
	for _, b := range slice {
		if b.Kind != schema.KindToolCall || b.ToolCall == nil {
			continue
		}
		switch b.ToolCall.Name {
		case grepToolName, globToolName:
			if p := searchPattern(b.ToolCall); p != "" {
				searches = append(searches, p)
			}
		case readToolName:
			res, ok := results[b.ToolCall.ToolUseID]
			if !ok || isFailure(res) {
				continue // unknown or failed read does not resolve the flail
			}
			path := readPath(b.ToolCall)
			if path != "" && len(searches) >= minSearchFlail && searchesNamePath(searches, path) {
				out = append(out, candidate{
					Kind:        PathFact,
					Value:       path,
					Fingerprint: pathFingerprint(path),
					Skill:       skillOf(b),
					Failed:      searches,
				})
			}
			searches = nil // a successful read ends the run
		}
	}
	return out
}

// searchesNamePath reports whether any search pattern shares a significant token
// with the read path — the meaningful trial-and-error link the path signal needs.
func searchesNamePath(searches []string, path string) bool {
	want := wordTokens(path)
	for _, p := range searches {
		if shares(want, wordTokens(p)) {
			return true
		}
	}
	return false
}

// wordTokens splits a string on non-alphanumeric runs and returns the lowercased
// tokens of length >= minToken, so a path's segments/basename and a pattern's
// words are compared on meaningful identifiers rather than punctuation or noise.
func wordTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(w) >= minToken {
			out[w] = true
		}
	}
	return out
}

// readPath extracts the file path from a read tool call's arguments.
func readPath(c *schema.ToolCallBody) string {
	var a struct {
		Path string `json:"path"`
	}
	if len(c.Arguments) > 0 {
		_ = json.Unmarshal(c.Arguments, &a)
	}
	return strings.TrimSpace(a.Path)
}

// searchPattern extracts the pattern from a grep/glob tool call's arguments.
func searchPattern(c *schema.ToolCallBody) string {
	var a struct {
		Pattern string `json:"pattern"`
	}
	if len(c.Arguments) > 0 {
		_ = json.Unmarshal(c.Arguments, &a)
	}
	return strings.TrimSpace(a.Pattern)
}

// envVarPatterns is the allow-list of stderr signatures that mechanically name a
// missing environment variable (D7: a small allow-list, never free-text parsing).
// Each captures an UPPER_SNAKE var name in group 1; the uppercase convention is
// itself the precision filter that keeps ordinary output from matching.
var envVarPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b([A-Z][A-Z0-9_]{2,})\b:?\s+unbound variable`),
	regexp.MustCompile(`\b([A-Z][A-Z0-9_]{2,})\b\s+(?:is\s+)?(?:not set|unset|required|missing)`),
	regexp.MustCompile(`(?:environment variable|env var)\s+"?\b([A-Z][A-Z0-9_]{2,})\b"?`),
}

// detectConfigKeys finds the config-key pattern: a shell run that fails while its
// output names a missing env var (via envVarPatterns), then a later successful
// shell run — the var was set in between, so it is the durable fact. Precision
// (D7): only an allow-listed stderr signature pends a key, so ordinary config
// reads are never flagged; a subsequent success is required to confirm the fix.
func detectConfigKeys(slice []schema.Block) []candidate {
	results := pairResults(slice)
	var out []candidate
	type miss struct{ name, cmd string }
	var pending []miss
	for _, b := range slice {
		if b.Kind != schema.KindToolCall || b.ToolCall == nil || b.ToolCall.Name != shellTool {
			continue
		}
		res, ok := results[b.ToolCall.ToolUseID]
		if !ok {
			continue
		}
		cmd := command(b.ToolCall)
		if isFailure(res) {
			for _, v := range missingEnvVars(resultText(res)) {
				pending = append(pending, miss{name: v, cmd: cmd})
			}
			continue
		}
		// A success: any var named by a prior failure is now considered resolved.
		for _, m := range pending {
			out = append(out, candidate{
				Kind:        ConfigFact,
				Value:       m.name,
				Fingerprint: configFingerprint(m.name),
				Skill:       skillOf(b),
				Failed:      []string{m.cmd},
			})
		}
		pending = nil
	}
	return out
}

// missingEnvVars returns the distinct env-var names an output names as missing,
// in first-seen order, applying the envVarPatterns allow-list.
func missingEnvVars(text string) []string {
	if text == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, re := range envVarPatterns {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if v := m[1]; !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// resultText is the searchable text of a tool result: explicit stderr/stdout
// fields plus any text content parts (the shell tool records combined output as
// content parts, so the named-var signatures live there in real sessions).
func resultText(r *schema.ToolResultBody) string {
	var b strings.Builder
	write := func(s string) {
		if s != "" {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	write(r.Stderr)
	write(r.Stdout)
	for _, p := range r.Content {
		write(p.Text)
	}
	return b.String()
}
