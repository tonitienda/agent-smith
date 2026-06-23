package mode

import "strings"

// Coding Mode is a layered opinion, most-specific wins (D-CODE-5): the baked-in
// house method (DefaultPhases) is the default, the process skill pack (AS-074)
// adjusts per-phase stance, and a project's memory files (AS-032) are the third
// layer — they can reorder phases, skip a phase, or add a project rule. This file
// is that third layer (AS-075).
//
// The customisation is declarative and read the same way other project config is
// (no code execution, consistent with the security posture): a fenced block in a
// memory file tagged `smith-method`, e.g.
//
//	```smith-method
//	phases: think, plan, implement, verify
//	skip: refactor
//	rule: require a ticket before any code
//	```
//
// Resolution is tolerant and additive (PRD D2): an unrecognised or malformed
// directive is ignored and the unspecified parts fall back to the default, so a
// partial customisation never fails the mode. The lifecycle core stays
// string-only (package contract): the parser takes raw memory text and returns
// phase names and rule strings — it never imports memory or skill content.

// methodFence is the info string that marks a memory fenced block as a Coding
// Mode method override. Matched case-insensitively against the text after the
// opening fence.
const methodFence = "smith-method"

// Override is a project's declarative method customisation, parsed from a single
// memory file's `smith-method` block. A zero Override is inert.
type Override struct {
	// Phases, when non-empty, replaces the phase order outright (reorder/skip in
	// one). Empty leaves the inherited order unchanged.
	Phases []string
	// Skip names phases to drop from the inherited order — the ergonomic way to
	// remove one phase without re-listing them all.
	Skip []string
	// Rules are project method rules surfaced in the mode panel; they also reach
	// the model directly, since the memory block carrying them is on the log.
	Rules []string
}

// Method is the resolved Coding Mode method after layering the baked-in default
// under every project override: the phase order the shell and tracker use, plus
// any project rules to surface.
type Method struct {
	Phases []string
	Rules  []string
}

// ResolveMethod layers memos (memory file contents, lowest precedence first) over
// the baked-in default and returns the resolved method. Each memo may carry one
// `smith-method` block; later memos win for the phase order, while skips and rules
// accumulate. Memos without a block leave the method unchanged, so absent any
// override the default house method is returned verbatim.
func ResolveMethod(def []string, memos []string) Method {
	phases := append([]string(nil), def...)
	var rules []string
	for _, memo := range memos {
		o := ParseOverride(memo)
		if len(o.Phases) > 0 {
			phases = append([]string(nil), o.Phases...)
		}
		phases = dropPhases(phases, o.Skip)
		rules = append(rules, o.Rules...)
	}
	return Method{Phases: phases, Rules: rules}
}

// ParseOverride extracts the first `smith-method` fenced block from content and
// parses its directives. A missing block (or one with no recognised directives)
// yields a zero Override, so non-method memory files cost nothing.
func ParseOverride(content string) Override {
	body, ok := methodBlock(content)
	if !ok {
		return Override{}
	}
	var o Override
	for _, line := range strings.Split(body, "\n") {
		key, val, found := strings.Cut(stripComment(line), ":")
		if !found {
			continue
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "phases":
			o.Phases = phaseList(val)
		case "skip":
			o.Skip = append(o.Skip, phaseList(val)...)
		case "rule", "rules":
			o.Rules = append(o.Rules, val)
		}
	}
	return o
}

// stripComment drops a trailing `#` comment from a directive line — a `#` that is
// at the line start or preceded by whitespace AND followed by whitespace or
// end-of-line, the `# comment` convention the example syntax advertises
// (`phases: think, plan   # reorder`). A `#` that is glued to a token (e.g. an
// issue ref like "#123" inside a rule) is preserved.
func stripComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		beforeOK := i == 0 || line[i-1] == ' ' || line[i-1] == '\t'
		afterOK := i+1 >= len(line) || line[i+1] == ' ' || line[i+1] == '\t'
		if beforeOK && afterOK {
			return line[:i]
		}
	}
	return line
}

// methodBlock returns the inner text of the first fenced block whose info string
// is methodFence (case-insensitive), and whether one was found.
func methodBlock(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		fence, info, ok := openingFence(lines[i])
		if !ok || !strings.EqualFold(info, methodFence) {
			continue
		}
		var body []string
		for j := i + 1; j < len(lines); j++ {
			if t := strings.TrimSpace(lines[j]); strings.HasPrefix(t, fence) {
				return strings.Join(body, "\n"), true
			}
			body = append(body, lines[j])
		}
		// Unterminated fence: take the rest of the file as the block body.
		return strings.Join(body, "\n"), true
	}
	return "", false
}

// openingFence reports whether line opens a fenced code block, returning the
// fence run (``` or longer) and the trimmed info string after it.
func openingFence(line string) (fence, info string, ok bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") {
		return "", "", false
	}
	n := len(t) - len(strings.TrimLeft(t, "`"))
	return t[:n], strings.TrimSpace(t[n:]), true
}

// phaseList splits a comma-separated directive value into canonical (lowercased,
// trimmed) phase names, dropping blanks.
func phaseList(val string) []string {
	var out []string
	for _, p := range strings.Split(val, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// dropPhases returns phases with every name in skip removed (case-insensitive),
// preserving order. An empty skip returns phases unchanged.
func dropPhases(phases, skip []string) []string {
	if len(skip) == 0 {
		return phases
	}
	out := make([]string, 0, len(phases))
	for _, p := range phases {
		if PhaseIndex(skip, p) < 0 {
			out = append(out, p)
		}
	}
	return out
}
