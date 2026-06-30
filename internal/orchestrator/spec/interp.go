package spec

import (
	"regexp"
	"strings"
)

// interpVar matches a single ${...} interpolation reference. The DSL allows
// these inside concurrency.key and step/hook `with:` values (§4.3, §4.6); the
// closed variable namespace is checked by validateInterpolation.
var interpVar = regexp.MustCompile(`\$\{([^}]*)\}`)

// interpVars returns every interpolation variable name referenced in s (the text
// inside each ${...}), in order of appearance.
func interpVars(s string) []string {
	var out []string
	for _, m := range interpVar.FindAllStringSubmatch(s, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// stripInterp removes every ${...} reference from s so a secret-pattern scan
// (rule 14) does not flag a legitimate ${secrets.foo} handle as plaintext.
func stripInterp(s string) string {
	return interpVar.ReplaceAllString(s, "")
}

// knownInterp reports whether an interpolation variable name is in the closed
// namespace (§4.3): ${repository}, ${org}, ${id}, ${trigger.inputs.*}, and
// ${secrets.*}. The caller layers the additional, context-specific checks
// (trigger inputs must be declared by every trigger — rule 15; secret scopes
// must be listed — rule 14).
func knownInterp(name string) bool {
	switch name {
	case "repository", "org", "id":
		return true
	}
	return strings.HasPrefix(name, "trigger.inputs.") || strings.HasPrefix(name, "secrets.")
}
