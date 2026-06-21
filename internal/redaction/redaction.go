// Package redaction implements best-effort redaction-at-capture (AS-115): it
// scrubs high-confidence secrets out of a block's body *before* the block enters
// the append-only log (PRD D3), so the raw secret never reaches disk. The scrub
// is structural and self-describing — the scrubbed body keeps its natural kind
// and stays live in the projection, and a record of what was removed is stamped
// into Block.Ext["redaction"] (the additive escape hatch, PRD D2) so replay and
// /insights see a *documented* redaction rather than silent loss.
//
// This is data minimization, not the erasure guarantee. False negatives are
// inherent (the spike, docs/design/compliance-archiving.md §2.2): the
// authoritative mechanism is crypto-shredding at the deferred paid archive tier
// (D8). Keeping API keys out of the log entirely is the job of OS-keychain
// storage (AS-017); this is defense-in-depth for the cases that slip through —
// a pasted credential, a tool that echoes a token.
//
// A *Redactor satisfies the eventlog.Redactor interface, so a face wires it onto
// the session log once and every Append is scrubbed at the single chokepoint.
package redaction

import (
	"encoding/json"
	"strings"

	"github.com/tonitienda/agent-smith/schema"
)

// Producer labels the redaction record's origin in Block.Ext["redaction"].
const Producer = "redaction"

// ExtKey is the Block.Ext key under which the structural redaction record is
// stored. It is part of the additive escape hatch (PRD D2): a consumer that does
// not recognize it ignores it.
const ExtKey = "redaction"

// Record is the self-describing redaction marker stamped into
// Block.Ext["redaction"] when a block is scrubbed. It records which rules fired
// and how many spans each removed — never the secret itself or its offsets — so
// the redaction is auditable without re-leaking what it hid.
type Record struct {
	Version  int            `json:"v"`
	Producer string         `json:"producer"`
	Rules    map[string]int `json:"rules"`
	Total    int            `json:"total"`
}

// Redactor scrubs secrets out of block bodies. It is immutable and safe for
// concurrent use once built. The zero value redacts nothing; use New.
type Redactor struct {
	rules []rule
}

// New builds a Redactor from validated config: the built-in high-confidence
// rules plus any user-supplied extra patterns. Returns a Redactor that scrubs
// nothing only if there are no rules at all (which cannot happen, since the
// built-ins are always present).
func New(rules []rule) *Redactor {
	return &Redactor{rules: rules}
}

// Default returns a Redactor with only the built-in high-confidence rules.
func Default() *Redactor { return &Redactor{rules: append([]rule(nil), builtinRules...)} }

// Redact returns a copy of b with secrets in its body replaced by self-describing
// placeholders, and reports whether anything was changed. When something is
// scrubbed, the returned block carries a structural Record under
// Ext["redaction"]. Body pointers that are modified are copied first, so the
// caller's original block is never mutated. A block with no scrubbable body, or
// no matches, is returned unchanged (changed=false).
func (r *Redactor) Redact(b schema.Block) (schema.Block, bool) {
	if r == nil || len(r.rules) == 0 {
		return b, false
	}
	counts := map[string]int{}

	if b.Text != nil {
		if t, parts, n := r.redactTextBody(b.Text, counts); n > 0 {
			nt := *b.Text
			nt.Text = t
			nt.Parts = parts
			b.Text = &nt
		}
	}
	if b.ToolCall != nil {
		args, n1 := r.redactRaw(b.ToolCall.Arguments, counts)
		rawArg, n2 := r.redactString(b.ToolCall.ArgumentsRaw, counts)
		if n1+n2 > 0 {
			nc := *b.ToolCall
			nc.Arguments = args
			nc.ArgumentsRaw = rawArg
			b.ToolCall = &nc
		}
	}
	if b.ToolResult != nil {
		out, n1 := r.redactString(b.ToolResult.Stdout, counts)
		errOut, n2 := r.redactString(b.ToolResult.Stderr, counts)
		content, n3 := r.redactParts(b.ToolResult.Content, counts)
		sc, n4 := r.redactRaw(b.ToolResult.StructuredContent, counts)
		if n1+n2+n3+n4 > 0 {
			nr := *b.ToolResult
			nr.Stdout = out
			nr.Stderr = errOut
			nr.Content = content
			nr.StructuredContent = sc
			b.ToolResult = &nr
		}
	}
	if b.FileRead != nil {
		if c, n := r.redactString(b.FileRead.Content, counts); n > 0 {
			nf := *b.FileRead
			nf.Content = c
			b.FileRead = &nf
		}
	}
	if b.Reasoning != nil {
		text, n1 := r.redactString(b.Reasoning.Text, counts)
		summary, n2 := r.redactStrings(b.Reasoning.Summary, counts)
		if n1+n2 > 0 {
			nr := *b.Reasoning
			nr.Text = text
			nr.Summary = summary
			b.Reasoning = &nr
		}
	}

	total := 0
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return b, false
	}
	b.Ext = stampRecord(b.Ext, Record{Version: 1, Producer: Producer, Rules: counts, Total: total})
	return b, true
}

// redactString applies every rule to s in order, accumulating per-rule counts,
// and returns the scrubbed string and how many spans were replaced overall.
func (r *Redactor) redactString(s string, counts map[string]int) (string, int) {
	if s == "" {
		return s, 0
	}
	total := 0
	for _, ru := range r.rules {
		out, n := applyRule(ru, s)
		if n > 0 {
			s = out
			counts[ru.name] += n
			total += n
		}
	}
	return s, total
}

// redactStrings scrubs each element of ss, copying the slice only when something
// changes so an untouched slice is returned as-is.
func (r *Redactor) redactStrings(ss []string, counts map[string]int) ([]string, int) {
	total := 0
	var out []string
	for i, s := range ss {
		red, n := r.redactString(s, counts)
		if n > 0 {
			if out == nil {
				out = append([]string(nil), ss...)
			}
			out[i] = red
			total += n
		}
	}
	if out == nil {
		return ss, 0
	}
	return out, total
}

// redactRaw scrubs a json.RawMessage as bytes. The placeholder is JSON-safe (no
// quote/backslash), so substituting inside a string value keeps the JSON valid.
func (r *Redactor) redactRaw(raw json.RawMessage, counts map[string]int) (json.RawMessage, int) {
	if len(raw) == 0 {
		return raw, 0
	}
	out, n := r.redactString(string(raw), counts)
	if n == 0 {
		return raw, 0
	}
	return json.RawMessage(out), n
}

// redactParts scrubs the Text of each multimodal part, copying the slice (and the
// touched parts) only when something changes.
func (r *Redactor) redactParts(parts []schema.Part, counts map[string]int) ([]schema.Part, int) {
	total := 0
	var out []schema.Part
	for i, p := range parts {
		red, n := r.redactString(p.Text, counts)
		if n > 0 {
			if out == nil {
				out = append([]schema.Part(nil), parts...)
			}
			out[i].Text = red
			total += n
		}
	}
	if out == nil {
		return parts, 0
	}
	return out, total
}

// redactTextBody scrubs the body text and any multimodal part text, returning the
// new text, the (possibly copied) parts, and the total spans removed.
func (r *Redactor) redactTextBody(t *schema.TextBody, counts map[string]int) (string, []schema.Part, int) {
	text, n1 := r.redactString(t.Text, counts)
	parts, n2 := r.redactParts(t.Parts, counts)
	return text, parts, n1 + n2
}

// applyRule replaces every match of ru in s with a labeled placeholder. When ru
// has a capturing group, only group 1 is replaced; otherwise the whole match is.
// It returns the rewritten string and the number of spans replaced.
func applyRule(ru rule, s string) (string, int) {
	idxs := ru.re.FindAllStringSubmatchIndex(s, -1)
	if len(idxs) == 0 {
		return s, 0
	}
	ph := placeholder(ru.name)
	var b strings.Builder
	last := 0
	for _, m := range idxs {
		start, end := m[0], m[1]
		// Prefer group 1 when the rule defines one and it participated in the match.
		if len(m) >= 4 && m[2] >= 0 {
			start, end = m[2], m[3]
		}
		b.WriteString(s[last:start])
		b.WriteString(ph)
		last = end
	}
	b.WriteString(s[last:])
	return b.String(), len(idxs)
}

// stampRecord returns a copy of ext with the redaction Record added under ExtKey.
// The input map is never mutated (it may be shared with the caller's block). A
// marshal failure is impossible for this fixed shape, so the error is dropped.
func stampRecord(ext map[string]json.RawMessage, rec Record) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(ext)+1)
	for k, v := range ext {
		out[k] = v
	}
	raw, _ := json.Marshal(rec)
	out[ExtKey] = raw
	return out
}
