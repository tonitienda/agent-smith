package redaction

import "regexp"

// rule is one redaction pattern. When re has a capturing group, only group 1 is
// replaced (so e.g. an Authorization header keeps its name and loses only the
// token); otherwise the whole match is replaced. name labels the rule in the
// structural redaction record (Block.Ext["redaction"].rules) and in the
// placeholder, so a redaction is always self-describing (PRD D0).
type rule struct {
	name string
	re   *regexp.Regexp
}

// builtinRules are the high-confidence secret patterns scrubbed by default once
// redaction is enabled. They are deliberately conservative — formats with a
// distinctive prefix and length — so the false-positive rate stays near zero;
// this is best-effort data minimization, never an erasure guarantee (the spike,
// docs/design/compliance-archiving.md §2.2). Order matters: more specific rules
// (e.g. sk-ant-) run before their broader cousins (sk-) so the narrower label
// wins and the broader rule never re-matches an already-placed placeholder.
var builtinRules = []rule{
	// PEM private-key blocks (RSA/EC/OPENSSH/PGP/…), header through footer.
	{"private_key", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)},
	// AWS access key IDs (long-term AKIA, temporary ASIA).
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	// GitHub personal-access / app / refresh tokens and fine-grained PATs.
	{"github_token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})\b`)},
	// Slack tokens (bot/user/app/refresh/legacy).
	{"slack_token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`)},
	// Google API keys.
	{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`)},
	// Anthropic keys (sk-ant-…) before the broader OpenAI sk- rule.
	{"anthropic_key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9\-_]{20,}`)},
	// OpenAI keys (sk-…, sk-proj-…).
	{"openai_key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}`)},
	// Bearer / Authorization credentials: keep the scheme/header, redact the token.
	// The token class covers base64url (-_) and standard base64 (+/=) so a padded
	// or +//-bearing token is redacted whole rather than truncated at the first
	// such byte (which would leak the tail).
	{"bearer_token", regexp.MustCompile(`(?i)(?:authorization|bearer)["'\s:=]+([A-Za-z0-9._+/=\-]{16,})`)},
}

// placeholder is the text a redacted span is replaced with. It carries the rule
// name so the scrub is legible in the transcript, and contains no quote or
// backslash so substituting it inside a JSON string body (tool arguments,
// structured content) never breaks the surrounding JSON.
func placeholder(name string) string { return "[REDACTED:" + name + "]" }
