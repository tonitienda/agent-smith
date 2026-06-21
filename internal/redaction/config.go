package redaction

import (
	"fmt"
	"regexp"
)

// configReader is the slice of the layered config this view reads; *config.Config
// satisfies it via Decode. Kept as a tiny consumer-side interface so this package
// owns the `redaction` paths and their parsing without importing internal/config
// (AS-093: typed config views over the layered substrate).
type configReader interface {
	Decode(path string, v any) (bool, error)
}

// Config is the validated redaction-at-capture settings read from the
// `redaction` section: whether the capture-time scrub is on, and any extra
// user-supplied secret patterns. Redaction is off by default — it is
// defense-in-depth best-effort data minimization (PRD D0), so a user opts in.
type Config struct {
	// Enabled turns on capture-time redaction. Off by default.
	Enabled bool
	// ExtraPatterns are user-supplied regexes scrubbed in addition to the built-in
	// high-confidence rules. Each is matched whole (no capture-group semantics).
	ExtraPatterns []string
}

// ConfigFrom reads the `redaction` section out of the layered config into a
// validated Config. A missing or malformed section yields the defaults (off) —
// the tolerate-but-warn rule (PRD D2): a typo must not block a session. The
// dotted paths live here, with the feature, not in the composition root.
func ConfigFrom(c configReader) (Config, []string) {
	var raw struct {
		Enabled       bool     `json:"enabled"`
		ExtraPatterns []string `json:"extra_patterns"`
	}
	if _, err := c.Decode("redaction", &raw); err != nil {
		return Config{}, []string{fmt.Sprintf("ignoring redaction config: %v", err)}
	}
	return Config{Enabled: raw.Enabled, ExtraPatterns: raw.ExtraPatterns}, nil
}

// Build assembles a Redactor from cfg: the built-in high-confidence rules plus
// each valid extra pattern (named custom_1, custom_2, …). An extra pattern that
// fails to compile is skipped with a warning rather than aborting (PRD D2), so a
// single bad regex never disables redaction for the whole session.
func Build(cfg Config) (*Redactor, []string) {
	rules := append([]rule(nil), builtinRules...)
	var warnings []string
	for i, pat := range cfg.ExtraPatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("ignoring redaction.extra_patterns[%d] %q: %v", i, pat, err))
			continue
		}
		rules = append(rules, rule{name: fmt.Sprintf("custom_%d", i+1), re: re})
	}
	return New(rules), warnings
}
