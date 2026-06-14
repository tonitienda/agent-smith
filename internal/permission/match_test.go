package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tonitienda/agent-smith/internal/tool"
)

func TestPrefixMatch(t *testing.T) {
	cases := []struct {
		pattern, subject string
		want             bool
	}{
		{"git status*", "git status", true},
		{"git status*", "git status -s", true},
		{"git status*", "git statusx", true}, // prefix, not word-boundary aware (documented)
		{"git status*", "git push", false},
		{"git status", "git status", true}, // exact, no trailing *
		{"git status", "git status -s", false},
		{"*", "anything at all", true},
		{"npm*", "  npm test", true}, // leading whitespace trimmed
	}
	for _, c := range cases {
		if got := prefixMatch(c.pattern, c.subject); got != c.want {
			t.Errorf("prefixMatch(%q, %q) = %v, want %v", c.pattern, c.subject, got, c.want)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},
		{"docs/*", "docs/README.md", true},
		{"docs/*", "docs/project/PRD.md", false}, // * does not span /
		{"docs/**", "docs/project/PRD.md", true}, // ** spans /
		{"docs/**", "docs", true},                // ** matches zero or more segments, so it covers the prefix itself
		{"**", "a/b/c", true},
		{"**/*.go", "internal/tool/runtime.go", true},
		{"cmd/*/main.go", "cmd/smith/main.go", true},
		{"cmd/*/main.go", "cmd/a/b/main.go", false},
		{"src/**/test_?.go", "src/a/b/test_x.go", true},
		{"exact", "exact", true},
		{"exact", "other", false},
		// ? matches one multi-byte rune, not one byte, for unicode filenames.
		{"caf?.txt", "café.txt", true},
		{"r?sum?", "résumé", true},
		{"*.md", "naïve.md", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestMatchesToolAndWildcard(t *testing.T) {
	p := New(Config{})
	call := tool.Call{Name: "write", Arguments: json.RawMessage(`{"path":"docs/x.md"}`)}

	tests := []struct {
		rule Rule
		want bool
	}{
		{Rule{Tool: "write", Pattern: "docs/**"}, true},
		{Rule{Tool: "write", Pattern: "src/**"}, false},
		{Rule{Tool: "write"}, true}, // empty pattern matches any write
		{Rule{Tool: "write", Pattern: "*"}, true},
		{Rule{Tool: "read", Pattern: "docs/**"}, false}, // wrong tool
		{Rule{Tool: "*", Pattern: "docs/**"}, true},     // wildcard tool
		{Rule{Tool: "*"}, true},
	}
	for _, tc := range tests {
		if got := p.matches(tc.rule, call); got != tc.want {
			t.Errorf("matches(%+v) = %v, want %v", tc.rule, got, tc.want)
		}
	}
}

func TestPatternNeedsKnownSubject(t *testing.T) {
	p := New(Config{})
	// "mytool" has no subjecter; a concrete pattern cannot match it, but a
	// whole-tool (empty pattern) rule can.
	call := tool.Call{Name: "mytool", Arguments: json.RawMessage(`{"x":"y"}`)}
	if p.matches(Rule{Tool: "mytool", Pattern: "y*"}, call) {
		t.Fatal("a pattern matched a tool with no subjecter")
	}
	if !p.matches(Rule{Tool: "mytool"}, call) {
		t.Fatal("a whole-tool rule failed to match")
	}
}

func TestWithSubjecterRegistersCustomTool(t *testing.T) {
	p := New(Config{DefaultMode: ModeAllowlist, Allow: []Rule{{Tool: "fetch", Pattern: "https://example.com/**"}}},
		WithSubjecter("fetch", "url", MatchGlob))
	allow := tool.Call{Name: "fetch", Arguments: json.RawMessage(`{"url":"https://example.com/a/b"}`)}
	deny := tool.Call{Name: "fetch", Arguments: json.RawMessage(`{"url":"https://evil.test/x"}`)}
	if d := p.decide(context.Background(), allow); !d.Allow {
		t.Fatalf("custom subjecter did not allow matching url: %+v", d)
	}
	if d := p.decide(context.Background(), deny); d.Allow {
		t.Fatalf("custom subjecter allowed a non-matching url")
	}
}
