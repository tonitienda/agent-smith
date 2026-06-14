package permission

import (
	"encoding/json"
	"strings"

	"github.com/tonitienda/agent-smith/internal/tool"
)

// MatchStyle is how an allow-rule pattern is matched against a call's subject.
type MatchStyle int

const (
	// MatchGlob matches paths: * and ? match within a path segment, ** spans
	// segments, and / is the segment separator (so docs/* does not match
	// docs/a/b, but docs/** does). Used for the file tools.
	MatchGlob MatchStyle = iota
	// MatchPrefix matches command lines by literal prefix: a pattern ending in *
	// matches any subject that starts with the text before it (git status* allows
	// git status and its arguments); a pattern without a trailing * must match
	// exactly. Used for the shell tool.
	MatchPrefix
)

// subjecter knows how to pull a rule-matchable subject out of a tool's
// arguments: field is the JSON property to read, style how a pattern matches it.
type subjecter struct {
	field string
	style MatchStyle
}

// defaultSubjecters maps the built-in tools (AS-014 file/search, AS-015 shell)
// to the argument field a pattern matches and how. Tools absent here have no
// subject: a rule with a non-empty pattern can never match them, while a
// pattern-less (whole-tool) rule still can.
func defaultSubjecters() map[string]subjecter {
	return map[string]subjecter{
		"shell": {field: "command", style: MatchPrefix},
		"read":  {field: "path", style: MatchGlob},
		"write": {field: "path", style: MatchGlob},
		"edit":  {field: "path", style: MatchGlob},
		"glob":  {field: "path", style: MatchGlob},
		"grep":  {field: "path", style: MatchGlob},
	}
}

// subjectValue extracts the call's subject string and reports whether the tool
// has a known subjecter. A tool with no subjecter, or whose subject field is
// absent or not a string, yields ("", false)/("", true) respectively: ok
// distinguishes "this tool has no subject concept" from "the field was empty".
func (p *Policy) subjectValue(call tool.Call) (string, bool) {
	p.mu.RLock()
	s, ok := p.subjects[call.Name]
	p.mu.RUnlock()
	if !ok {
		return "", false
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", true
	}
	raw, present := args[s.field]
	if !present {
		return "", true
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", true
	}
	return v, true
}

// matches reports whether rule r matches call. The tool must match (or r.Tool is
// the wildcard "*"); an empty or "*" pattern matches any call of that tool
// regardless of arguments. A concrete pattern needs a subject, so it cannot
// match a tool with no subjecter.
func (p *Policy) matches(r Rule, call tool.Call) bool {
	if r.Tool != "*" && r.Tool != call.Name {
		return false
	}
	if r.Pattern == "" || r.Pattern == "*" {
		return true
	}
	subject, ok := p.subjectValue(call)
	if !ok || subject == "" {
		return false
	}
	style := MatchGlob
	p.mu.RLock()
	if s, found := p.subjects[call.Name]; found {
		style = s.style
	}
	p.mu.RUnlock()

	if style == MatchPrefix {
		return prefixMatch(r.Pattern, subject)
	}
	return globMatch(r.Pattern, subject)
}

// prefixMatch implements MatchPrefix: a trailing * makes the pattern a literal
// prefix; otherwise the subject must equal the pattern. The subject's leading
// whitespace is trimmed so " git status" still matches "git status*".
func prefixMatch(pattern, subject string) bool {
	subject = strings.TrimLeft(subject, " \t")
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(subject, strings.TrimSuffix(pattern, "*"))
	}
	return subject == pattern
}

// globMatch implements MatchGlob over /-separated paths. Each pattern segment is
// matched against a name segment with segMatch; a ** segment matches zero or
// more whole segments.
func globMatch(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchSegments matches the remaining pattern segments against the remaining
// name segments, expanding a ** segment to zero-or-more name segments.
func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive ** and a trailing ** matches everything left.
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if !segMatch(pat[0], name[0]) {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// segMatch matches a single path segment against a glob segment where * matches
// any run of characters and ? matches one. It compares runes, not bytes, so ?
// matches a single multi-byte character (a unicode filename) rather than one
// byte of it. It uses backtracking on *, which is linear in practice for these
// short segments.
func segMatch(pat, s string) bool {
	p := []rune(pat)
	str := []rune(s)
	var pi, si, star, mark int
	star = -1
	for si < len(str) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == str[si]):
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			star = pi
			mark = si
			pi++
		case star != -1:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
