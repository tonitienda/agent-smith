// Package skillcontract is the declarative half of living skills (AS-047, PRD
// §7.20, Appendix C.1): it parses the expectation contract a skill may declare
// in its SKILL.md frontmatter and tracks a skill activation's span over the
// append-only event log. It is pure plumbing — no judgment, no model calls — and
// the foundation the rediscovered-fact detector (AS-048) and the
// skill-expectation analyzer (AS-049) build on.
//
// Two pieces live here:
//
//   - Contract / ParseContract — the typed C.1 contract (expected outcome, effort
//     budget, should-not-rediscover facts, success signals, completion trigger)
//     and a tolerant parser for the YAML subset that contract uses. A skill that
//     declares no contract fields parses to a zero Contract (Declared == false)
//     without complaint; inferring a contract for such a skill is AS-049's job.
//   - Tracker / Span — span tracking from skill activation to teardown, fired by
//     the declared completion.signal when present, else the idle_turns heuristic
//     (Q10: declared preferred, heuristic v1 fallback), accumulating per-span
//     actuals (tool calls, turns, cost) attributed to the right skill even with
//     overlapping activations (see Tracker for the attribution rule).
//
// The package depends only on the stdlib, the frozen block schema, and the event
// log's skill-load kind, so it stays face- and runtime-agnostic: a consumer feeds
// it the contract text and the blocks as they are appended, and reads back spans.
package skillcontract

import (
	"strconv"
	"strings"
)

// EffortBudget is the soft target a skill declares for its in-scope span
// (Appendix C.1). Every field is a soft target, never a gate: zero means
// "unspecified" rather than "a budget of zero".
type EffortBudget struct {
	ToolCalls  int     // soft target tool-call count for the span
	Turns      int     // soft target turn count for the span
	MaxCostUSD float64 // optional soft cost ceiling; 0 means unspecified
}

// ExpectedOutcome is the declared `expected_outcome` block (Appendix C.1): what
// the skill should accomplish, its rough budget, the facts it already encodes
// (rediscovery of which signals a content gap), and optional success signals.
type ExpectedOutcome struct {
	Summary             string
	EffortBudget        EffortBudget
	ShouldNotRediscover []string
	SuccessSignals      []string
}

// Completion declares when a skill's span ends (Appendix C.1), driving analyzer
// teardown. Signal is the preferred declared trigger; IdleTurns is the v1
// heuristic fallback fired when no signal is declared (or before one fires).
type Completion struct {
	Signal    string // declared teardown signal (preferred); empty means none declared
	IdleTurns int    // heuristic fallback: teardown after N turns without skill tool use; 0 disables
}

// Contract is a skill's parsed C.1 expectation contract. Declared reports whether
// the skill carried any contract fields at all: a skill with none parses to a
// zero Contract with Declared == false, which AS-049 later fills by inference.
type Contract struct {
	Declared        bool
	ExpectedOutcome ExpectedOutcome
	Completion      Completion
}

// ParseContract reads the C.1 contract out of a SKILL.md frontmatter body (the
// text between the `---` fences, without the fences). It is tolerant by design:
// unknown top-level keys (name, description, …) are ignored, every contract field
// is optional, and malformed numbers degrade to zero rather than failing — a
// skill never fails to load because its contract is partial. Only the
// `expected_outcome` and `completion` sections are interpreted.
//
// The accepted shape is the C.1 YAML subset: two-space-indented nested maps,
// `- item` lists, scalar `key: value` pairs, optional double-quoted values, and
// trailing `# comments`. It is not a general YAML parser.
func ParseContract(frontmatter string) Contract {
	var c Contract
	lines := strings.Split(strings.ReplaceAll(frontmatter, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if indentOf(line) != 0 || isBlankOrComment(line) {
			continue // child line consumed by a section below, or noise
		}
		key, _, ok := cutKey(line)
		if !ok {
			continue
		}
		switch key {
		case "expected_outcome":
			block, next := childLines(lines, i+1, 0)
			c.ExpectedOutcome = parseExpectedOutcome(block)
			c.Declared = true
			i = next - 1
		case "completion":
			block, next := childLines(lines, i+1, 0)
			c.Completion = parseCompletion(block)
			c.Declared = true
			i = next - 1
		}
	}
	return c
}

// parseExpectedOutcome parses the indented body of an `expected_outcome:` section.
func parseExpectedOutcome(lines []string) ExpectedOutcome {
	var eo ExpectedOutcome
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if isBlankOrComment(line) || indentOf(line) != 2 {
			continue
		}
		key, val, ok := cutKey(line)
		if !ok {
			continue
		}
		switch key {
		case "summary":
			eo.Summary = scalar(val)
		case "effort_budget":
			body, _ := childLines(lines, i+1, 2)
			eo.EffortBudget = parseEffortBudget(body)
		case "should_not_rediscover":
			body, _ := childLines(lines, i+1, 2)
			eo.ShouldNotRediscover = listItems(body)
		case "success_signals":
			body, _ := childLines(lines, i+1, 2)
			eo.SuccessSignals = listItems(body)
		}
	}
	return eo
}

// parseEffortBudget parses the indented body of an `effort_budget:` section.
func parseEffortBudget(lines []string) EffortBudget {
	var b EffortBudget
	for _, line := range lines {
		if isBlankOrComment(line) {
			continue
		}
		key, val, ok := cutKey(line)
		if !ok {
			continue
		}
		switch key {
		case "tool_calls":
			b.ToolCalls = atoi(scalar(val))
		case "turns":
			b.Turns = atoi(scalar(val))
		case "max_cost_usd":
			b.MaxCostUSD = atof(scalar(val))
		}
	}
	return b
}

// parseCompletion parses the indented body of a `completion:` section.
func parseCompletion(lines []string) Completion {
	var c Completion
	for _, line := range lines {
		if isBlankOrComment(line) {
			continue
		}
		key, val, ok := cutKey(line)
		if !ok {
			continue
		}
		switch key {
		case "signal":
			c.Signal = scalar(val)
		case "idle_turns":
			c.IdleTurns = atoi(scalar(val))
		}
	}
	return c
}

// childLines returns the run of lines starting at start that are indented deeper
// than parentIndent (the children of the preceding key at parentIndent), together
// with the index of the first line that ends the run. A blank or comment line
// does not end a section — only a non-blank line at or below parentIndent (or end
// of input) does — so a sibling key at the same indent terminates the section.
func childLines(lines []string, start, parentIndent int) (block []string, next int) {
	i := start
	for i < len(lines) {
		if !isBlankOrComment(lines[i]) && indentOf(lines[i]) <= parentIndent {
			break
		}
		i++
	}
	return lines[start:i], i
}

// listItems extracts `- value` entries from a section body, ignoring anything
// that is not a list item so a stray scalar line cannot corrupt the list.
func listItems(lines []string) []string {
	var out []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		if v := scalar(strings.TrimPrefix(t, "- ")); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// cutKey splits a `key: value` line into its trimmed key and the raw remainder.
// It reports ok == false for a line with no colon (e.g. a list item).
func cutKey(line string) (key, rest string, ok bool) {
	k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(k), v, true
}

// scalar normalizes a raw scalar value: a double-quoted value yields its quoted
// contents verbatim (so a signal like "`make ship` exited 0" keeps its
// backticks); an unquoted value is trimmed and has any trailing ` # comment`
// stripped.
func scalar(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, `"`) {
		if end := strings.Index(raw[1:], `"`); end >= 0 {
			return raw[1 : 1+end]
		}
		return strings.TrimPrefix(raw, `"`)
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

// indentOf counts a line's leading spaces. Tabs are treated as a single space so
// a tab-indented child is still recognized as a child rather than a top-level key.
func indentOf(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ', '\t':
			n++
		default:
			return n
		}
	}
	return n
}

// isBlankOrComment reports whether a line is empty or a whole-line `# comment`.
func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
