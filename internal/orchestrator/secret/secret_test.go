package secret

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		scope string
		want  Class
		ok    bool
	}{
		{"anthropic-api-key", ClassProvider, true},
		{"openai-api-key", ClassProvider, true},
		{"github-token", ClassGitHub, true},
		{"smith-service", ClassService, true},
		{"user.deploy-token", ClassUser, true},
		{"user.", "", false}, // bare prefix is not a scope
		{"mystery", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := Classify(c.scope)
		if got != c.want || ok != c.ok {
			t.Errorf("Classify(%q) = (%q, %v), want (%q, %v)", c.scope, got, ok, c.want, c.ok)
		}
	}
}

func TestValidateScopes(t *testing.T) {
	unknown := ValidateScopes([]string{"github-token", "zeta", "anthropic-api-key", "alpha"})
	if len(unknown) != 2 || unknown[0] != "alpha" || unknown[1] != "zeta" {
		t.Fatalf("ValidateScopes returned %v, want sorted [alpha zeta]", unknown)
	}
	if got := ValidateScopes([]string{"github-token", "user.x"}); got != nil {
		t.Fatalf("ValidateScopes over known scopes = %v, want nil", got)
	}
}

// A Value must refuse to render its bytes through every common leak channel: fmt
// verbs, JSON encoding, and struct embedding.
func TestValueNeverRenders(t *testing.T) {
	v := NewValue("github-token", "ghp_supersecrettoken0000")
	for _, s := range []string{
		v.String(),
		fmt.Sprintf("%v", v),
		fmt.Sprintf("x=%s", v),
		fmt.Sprintf("%#v", v),
		fmt.Sprintf("token=%v", v),
	} {
		if strings.Contains(s, "supersecret") {
			t.Fatalf("rendered value leaked the secret: %q", s)
		}
		if !strings.Contains(s, "[REDACTED]") {
			t.Fatalf("rendered value missing placeholder: %q", s)
		}
	}

	b, err := json.Marshal(struct {
		Token Value `json:"token"`
	}{v})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "supersecret") {
		t.Fatalf("JSON leaked the secret: %s", b)
	}
	if !strings.Contains(string(b), "[REDACTED]") {
		t.Fatalf("JSON missing placeholder: %s", b)
	}

	if v.Reveal() != "ghp_supersecrettoken0000" {
		t.Fatal("Reveal must return the raw bytes")
	}
	if v.Scope() != "github-token" {
		t.Fatalf("Scope = %q", v.Scope())
	}
}

func TestMapResolver(t *testing.T) {
	r := MapResolver{"github-token": "ghp_x", "empty": ""}
	got, err := r.Resolve("github-token")
	if err != nil || got.Reveal() != "ghp_x" {
		t.Fatalf("Resolve(github-token) = (%q, %v)", got.Reveal(), err)
	}
	if _, err := r.Resolve("empty"); err == nil {
		t.Fatal("empty value must fail closed")
	}
	if _, err := r.Resolve("missing"); err == nil {
		t.Fatal("missing scope must fail closed")
	}
}

// An AuditRecord must carry identity/class/recipient/run/expiry but never a
// value — verified structurally (no value field) and via JSON (no secret bytes).
func TestAuditRecordHasNoValue(t *testing.T) {
	v := NewValue("github-token", "ghp_supersecrettoken0000")
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	exp := at.Add(time.Hour)
	rec := Audit(v, "github", "run-123", exp, at)

	if rec.Name != "github-token" || rec.Scope != "github-token" {
		t.Fatalf("record identity wrong: %+v", rec)
	}
	if rec.Class != ClassGitHub {
		t.Fatalf("record class = %q, want github", rec.Class)
	}
	if rec.Recipient != "github" || rec.RunID != "run-123" {
		t.Fatalf("record recipient/run wrong: %+v", rec)
	}
	if !rec.Expiry.Equal(exp) || !rec.At.Equal(at) {
		t.Fatalf("record times wrong: %+v", rec)
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "supersecret") {
		t.Fatalf("audit record leaked the value: %s", b)
	}
}

func TestRedactor(t *testing.T) {
	a := NewValue("github-token", "ghp_aaaa")
	b := NewValue("anthropic-api-key", "sk-ant-bbbbbbbb")
	empty := NewValue("user.x", "")
	r := NewRedactor(a, b, empty)

	in := "cloning with ghp_aaaa then calling sk-ant-bbbbbbbb ok"
	out := r.Redact(in)
	if strings.Contains(out, "ghp_aaaa") || strings.Contains(out, "sk-ant-bbbbbbbb") {
		t.Fatalf("redaction left a secret: %q", out)
	}
	if strings.Count(out, "[REDACTED]") != 2 {
		t.Fatalf("expected 2 placeholders, got %q", out)
	}

	// The empty value must not turn into a match-everything placeholder.
	if got := r.Redact("nothing here"); got != "nothing here" {
		t.Fatalf("empty value caused spurious redaction: %q", got)
	}

	if got := string(r.RedactBytes([]byte("ghp_aaaa"))); got != "[REDACTED]" {
		t.Fatalf("RedactBytes = %q", got)
	}

	var nilR *Redactor
	if got := nilR.Redact("ghp_aaaa"); got != "ghp_aaaa" {
		t.Fatalf("nil redactor must be a no-op, got %q", got)
	}
}

// A longer secret that contains a shorter one must be fully redacted (longest
// match first), not partially revealed.
func TestRedactorLongestFirst(t *testing.T) {
	short := NewValue("user.a", "secret")
	long := NewValue("user.b", "secret-extended-value")
	r := NewRedactor(short, long)
	out := r.Redact("here is secret-extended-value done")
	if strings.Contains(out, "extended") {
		t.Fatalf("longer secret partially revealed: %q", out)
	}
}
