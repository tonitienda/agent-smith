package redaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// recordOf decodes the structural redaction record stamped on a scrubbed block,
// failing the test if it is missing or malformed.
func recordOf(t *testing.T, b schema.Block) Record {
	t.Helper()
	raw, ok := b.Ext[ExtKey]
	if !ok {
		t.Fatalf("block %s carries no %q ext record", b.ID, ExtKey)
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode redaction record: %v", err)
	}
	return rec
}

func TestRedactHighConfidenceSecrets(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		rule   string
		absent string // the raw secret that must not survive
	}{
		{"openai", "key is sk-proj-abcdEFGH1234567890 zzz", "openai_key", "sk-proj-abcdEFGH1234567890"},
		{"anthropic", "use sk-ant-api03-AbCdEfGh1234567890xyz now", "anthropic_key", "sk-ant-api03-AbCdEfGh1234567890xyz"},
		{"github", "token ghp_0123456789abcdefghij0123456789abcdef done", "github_token", "ghp_0123456789abcdefghij0123456789abcdef"},
		{"aws", "id AKIAIOSFODNN7EXAMPLE here", "aws_access_key", "AKIAIOSFODNN7EXAMPLE"},
		{"google", "AIzaSyA1234567890abcdefghijklmnopqrstuv key", "google_api_key", "AIzaSyA1234567890abcdefghijklmnopqrstuv"},
		{"slack", "xoxb-1234567890-ABCDEFabcdef token", "slack_token", "xoxb-1234567890-ABCDEFabcdef"},
		{"bearer", "Authorization: Bearer abcDEF1234567890ghiJKL", "bearer_token", "abcDEF1234567890ghiJKL"},
		{"bearer_base64", "Authorization: Bearer abc/def+ghi=jklMNO123", "bearer_token", "abc/def+ghi=jklMNO123"},
	}
	r := Default()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := schema.Block{ID: "blk", Kind: schema.KindText, Text: &schema.TextBody{Text: tc.in}}
			out, changed := r.Redact(b)
			if !changed {
				t.Fatalf("expected redaction for %q", tc.in)
			}
			if strings.Contains(out.Text.Text, tc.absent) {
				t.Fatalf("secret survived: %q", out.Text.Text)
			}
			if !strings.Contains(out.Text.Text, placeholder(tc.rule)) {
				t.Fatalf("expected placeholder %q in %q", placeholder(tc.rule), out.Text.Text)
			}
			rec := recordOf(t, out)
			if rec.Rules[tc.rule] == 0 {
				t.Fatalf("record missing rule %q: %+v", tc.rule, rec)
			}
			if rec.Total < 1 || rec.Version != 1 || rec.Producer != Producer {
				t.Fatalf("unexpected record: %+v", rec)
			}
		})
	}
}

func TestRedactBearerKeepsHeaderName(t *testing.T) {
	r := Default()
	b := schema.Block{ID: "blk", Kind: schema.KindText, Text: &schema.TextBody{Text: "Authorization: Bearer abcDEF1234567890ghiJKL"}}
	out, _ := r.Redact(b)
	if !strings.Contains(out.Text.Text, "Authorization") {
		t.Fatalf("header name should survive: %q", out.Text.Text)
	}
}

func TestRedactNoMatchUnchanged(t *testing.T) {
	r := Default()
	b := schema.Block{ID: "blk", Kind: schema.KindText, Text: &schema.TextBody{Text: "the quick brown fox"}}
	out, changed := r.Redact(b)
	if changed {
		t.Fatalf("expected no redaction, got %q", out.Text.Text)
	}
	if _, ok := out.Ext[ExtKey]; ok {
		t.Fatalf("no record should be stamped when nothing changed")
	}
}

func TestRedactDoesNotMutateOriginal(t *testing.T) {
	r := Default()
	orig := &schema.TextBody{Text: "key sk-proj-abcdEFGH1234567890 end"}
	b := schema.Block{ID: "blk", Kind: schema.KindText, Text: orig}
	_, changed := r.Redact(b)
	if !changed {
		t.Fatal("expected redaction")
	}
	if !strings.Contains(orig.Text, "sk-proj-abcdEFGH1234567890") {
		t.Fatalf("caller's original body was mutated: %q", orig.Text)
	}
}

func TestRedactToolResultStreams(t *testing.T) {
	r := Default()
	b := schema.Block{
		ID:   "blk",
		Kind: schema.KindToolResult,
		ToolResult: &schema.ToolResultBody{
			ToolUseID: "t1",
			Stdout:    "exported GITHUB_TOKEN=ghp_0123456789abcdefghij0123456789abcdef",
			Stderr:    "no secret here",
		},
	}
	out, changed := r.Redact(b)
	if !changed {
		t.Fatal("expected redaction in stdout")
	}
	if strings.Contains(out.ToolResult.Stdout, "ghp_0123456789abcdefghij0123456789abcdef") {
		t.Fatalf("secret survived in stdout: %q", out.ToolResult.Stdout)
	}
	if out.ToolResult.Stderr != "no secret here" {
		t.Fatalf("stderr should be untouched: %q", out.ToolResult.Stderr)
	}
}

func TestRedactToolCallArgsStayValidJSON(t *testing.T) {
	r := Default()
	args := json.RawMessage(`{"cmd":"curl -H 'Authorization: Bearer abcDEF1234567890ghiJKL'"}`)
	b := schema.Block{
		ID:       "blk",
		Kind:     schema.KindToolCall,
		ToolCall: &schema.ToolCallBody{ToolUseID: "t1", Name: "shell", Arguments: args},
	}
	out, changed := r.Redact(b)
	if !changed {
		t.Fatal("expected redaction in arguments")
	}
	var v map[string]any
	if err := json.Unmarshal(out.ToolCall.Arguments, &v); err != nil {
		t.Fatalf("redacted arguments are not valid JSON: %v (%s)", err, out.ToolCall.Arguments)
	}
	if strings.Contains(string(out.ToolCall.Arguments), "abcDEF1234567890ghiJKL") {
		t.Fatalf("secret survived in arguments: %s", out.ToolCall.Arguments)
	}
}

func TestBuildExtraPatternsAndBadRegex(t *testing.T) {
	cfg := Config{Enabled: true, ExtraPatterns: []string{`COMPANY-[0-9]{6}`, `(`}}
	r, warns := Build(cfg)
	if len(warns) != 1 {
		t.Fatalf("expected one warning for the bad regex, got %v", warns)
	}
	b := schema.Block{ID: "blk", Kind: schema.KindText, Text: &schema.TextBody{Text: "ref COMPANY-123456 ok"}}
	out, changed := r.Redact(b)
	if !changed || strings.Contains(out.Text.Text, "COMPANY-123456") {
		t.Fatalf("extra pattern should redact: %q", out.Text.Text)
	}
	if !strings.Contains(out.Text.Text, placeholder("custom_1")) {
		t.Fatalf("expected custom_1 placeholder: %q", out.Text.Text)
	}
}

func TestNilRedactorNoChange(t *testing.T) {
	var r *Redactor
	b := schema.Block{ID: "blk", Kind: schema.KindText, Text: &schema.TextBody{Text: "sk-proj-abcdEFGH1234567890"}}
	if _, changed := r.Redact(b); changed {
		t.Fatal("nil redactor must not change anything")
	}
}
