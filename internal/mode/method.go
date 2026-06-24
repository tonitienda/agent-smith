package mode

import (
	"strings"
	"unicode"
)

// The opinionated method is layered, most-specific wins (D-CODE-5): the baked-in
// house method (DefaultPhases) → the process skill pack (AS-074) → project memory
// (AS-075, this file). A project customises the phase sequence declaratively by
// embedding a fenced `smith-method` block in one of its memory files (CLAUDE.md /
// AGENTS.md / AGENT.md, AS-032), e.g.:
//
//	```smith-method
//	phases: think, plan, implement, verify
//	```
//
// Listing the phases is how a project reorders, skips ("this repo skips
// refactor" — just omit it), or extends the method (add a custom phase name).
// Resolution is declarative and tolerant (D-CODE-5.3, security posture): the
// block is data read from memory like any other project config — no code runs —
// and a malformed or empty directive degrades to the default for the unspecified
// parts rather than failing the mode. Per-phase prose rules and stance live in
// the memory file's ordinary text, which is already model-facing context; the
// structured directive governs only the phase sequence the tracker and shell
// (AS-072/AS-073) mechanically consume.

// MethodFence is the markdown fenced-code-block info string a memory file uses to
// carry a Coding Mode method override (AS-075).
const MethodFence = "smith-method"

// ResolvePhases returns the Coding Mode phase list after applying any project
// method override found in memoryTexts — the contents of the loaded memory files
// (AS-032), passed lowest-precedence first. With no valid override it returns
// DefaultPhases() unchanged. Resolution is tolerant: a malformed or empty
// override is ignored (degrading to the default), and when several memory files
// carry a valid override the most specific (last) one wins.
func ResolvePhases(memoryTexts []string) []string {
	phases := DefaultPhases()
	for _, text := range memoryTexts {
		if p, ok := parseMethodOverride(text); ok {
			phases = p
		}
	}
	return phases
}

// parseMethodOverride extracts the phase list from the last valid `smith-method`
// fenced block in text, or false when text carries no usable override. It scans
// for a fence opened by MethodFence (``` or ~~~) and reads a `phases:` line
// inside it; a block whose phases line is absent or empty contributes nothing, so
// a partial or malformed directive degrades to the default rather than erroring.
func parseMethodOverride(text string) ([]string, bool) {
	var phases []string
	found := false
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if !inBlock {
			if t == "```"+MethodFence || t == "~~~"+MethodFence {
				inBlock = true
			}
			continue
		}
		if t == "```" || t == "~~~" {
			inBlock = false
			continue
		}
		if rest, ok := cutKey(t, "phases"); ok {
			if p := splitPhases(rest); len(p) > 0 {
				phases, found = p, true
			}
		}
	}
	return phases, found
}

// cutKey returns the value after the first colon when line is a `key: value`
// pair whose key (case-insensitively) matches key.
func cutKey(line, key string) (string, bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(line[:i]), key) {
		return "", false
	}
	return line[i+1:], true
}

// splitPhases parses a phase list value, accepting commas and/or whitespace as
// separators. Names are lowercased to the canonical spelling (DefaultPhases is
// lowercase) and de-duplicated so a stray repeat does not create a junk phase.
func splitPhases(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	var out []string
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.ToLower(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}
