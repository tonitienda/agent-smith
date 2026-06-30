package spec

import (
	"regexp"
	"strings"
)

// secretPatterns is the best-effort plaintext-credential detector behind rule
// 14: a spec declares secret *scope names* only, never values, so a literal
// that looks like a real credential is rejected at load (fail-closed; the
// authoritative redaction backend is AS-154/AS-115). The list errs toward
// well-known, high-signal prefixes/shapes to avoid false positives on ordinary
// config text — it is a guardrail, not a vault.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`),                  // GitHub PAT / OAuth / app tokens
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),                // fine-grained GitHub PAT
	regexp.MustCompile(`sk-[A-Za-z0-9-]{20,}`),                        // OpenAI-style secret key
	regexp.MustCompile(`sk-ant-[A-Za-z0-9-]{20,}`),                    // Anthropic-style secret key
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                            // AWS access key id
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                // Slack token
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),          // PEM private key
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.`), // JWT
}

// looksLikeSecret reports whether s contains a substring matching a known
// credential shape. A ${secrets.*} reference is *not* a secret value (it carries
// a handle, not plaintext), so callers strip interpolation before checking.
func looksLikeSecret(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	for _, re := range secretPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
