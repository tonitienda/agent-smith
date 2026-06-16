package command

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// noop is a trivial handler for registration tests.
func noop(context.Context, []string) (Output, error) { return Output{}, nil }

func mustRegister(t *testing.T, r *Registry, cmds ...Command) {
	t.Helper()
	for _, c := range cmds {
		if err := r.Register(c); err != nil {
			t.Fatalf("Register(%q): %v", c.Name, err)
		}
	}
}

func TestRegisterRejectsBadCommands(t *testing.T) {
	r := NewRegistry()
	cases := []struct {
		name string
		cmd  Command
	}{
		{"empty name", Command{Run: noop}},
		{"leading slash", Command{Name: "/cost", Run: noop}},
		{"whitespace", Command{Name: "co st", Run: noop}},
		{"nil handler", Command{Name: "cost"}},
		{"interactive-only without reason", Command{Name: "clear", Scriptability: InteractiveOnly, Run: noop}},
	}
	for _, tc := range cases {
		if err := r.Register(tc.cmd); err == nil {
			t.Errorf("%s: Register succeeded, want error", tc.name)
		}
	}

	mustRegister(t, r, Command{Name: "cost", Run: noop})
	if err := r.Register(Command{Name: "cost", Run: noop}); err == nil {
		t.Error("duplicate name: Register succeeded, want error")
	}
}

func TestAllSortedByName(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r,
		Command{Name: "model", Run: noop},
		Command{Name: "clean", Run: noop},
		Command{Name: "cost", Run: noop},
	)
	got := []string{}
	for _, c := range r.All() {
		got = append(got, c.Name)
	}
	want := []string{"clean", "cost", "model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
}

func TestMatchFiltersAndRanks(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r,
		Command{Name: "cost", Run: noop},
		Command{Name: "context", Run: noop},
		Command{Name: "clean", Run: noop},
		Command{Name: "model", Run: noop},
	)

	// Empty query lists everything (sorted).
	if got := len(r.Match("")); got != 4 {
		t.Fatalf("Match(\"\") returned %d commands, want 4", got)
	}

	// Prefix narrows and ranks the shorter/closer name first.
	got := names(r.Match("co"))
	if len(got) != 2 || got[0] != "cost" {
		t.Fatalf("Match(\"co\") = %v, want [cost context]", got)
	}

	// An exact name wins outright even against a prefix of a longer one.
	if got := names(r.Match("cost")); got[0] != "cost" {
		t.Fatalf("Match(\"cost\")[0] = %q, want cost", got[0])
	}

	// A non-subsequence query matches nothing.
	if got := r.Match("zzz"); len(got) != 0 {
		t.Fatalf("Match(\"zzz\") = %v, want empty", names(got))
	}

	// Case-insensitive.
	if got := names(r.Match("MOD")); len(got) != 1 || got[0] != "model" {
		t.Fatalf("Match(\"MOD\") = %v, want [model]", got)
	}
}

func TestMatchNonASCIINames(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r,
		Command{Name: "café", Run: noop},
		Command{Name: "naïve", Run: noop},
	)
	// A multi-byte subsequence query matches by rune, not byte.
	if got := names(r.Match("cé")); len(got) != 1 || got[0] != "café" {
		t.Fatalf("Match(\"cé\") = %v, want [café]", got)
	}
	if got := names(r.Match("café")); len(got) != 1 || got[0] != "café" {
		t.Fatalf("Match(\"café\") = %v, want [café]", got)
	}
}

func TestSuggestNearestMatch(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r,
		Command{Name: "cost", Run: noop},
		Command{Name: "context", Run: noop},
		Command{Name: "model", Run: noop},
	)

	if s, ok := r.Suggest("cesty"); !ok || s != "cost" {
		t.Fatalf("Suggest(\"cesty\") = %q,%v, want cost,true", s, ok)
	}
	// Far-off garbage yields no suggestion rather than a misleading one.
	if s, ok := r.Suggest("xqzptv"); ok {
		t.Fatalf("Suggest(\"xqzptv\") = %q,true, want no suggestion", s)
	}
}

func TestParseQuotedArguments(t *testing.T) {
	cases := []struct {
		line     string
		wantName string
		wantArgs []string
	}{
		{`/clean "old api"`, "clean", []string{"old api"}},
		{`clean foo bar`, "clean", []string{"foo", "bar"}},
		{`/model claude-opus-4-8`, "model", []string{"claude-opus-4-8"}},
		{`/clean "a b" c`, "clean", []string{"a b", "c"}},
		{`/say "with \"quotes\""`, "say", []string{`with "quotes"`}},
		{`/clean "café münchen"`, "clean", []string{"café münchen"}}, // multi-byte runes survive
		{`/clean   spaced   args `, "clean", []string{"spaced", "args"}},
		{`/empty ""`, "empty", []string{""}},
	}
	for _, tc := range cases {
		name, args, err := Parse(tc.line)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tc.line, err)
			continue
		}
		if name != tc.wantName || !reflect.DeepEqual(args, tc.wantArgs) {
			t.Errorf("Parse(%q) = %q,%v, want %q,%v", tc.line, name, args, tc.wantName, tc.wantArgs)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, line := range []string{``, `   `, `/`, `"unterminated`} {
		if _, _, err := Parse(line); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", line)
		}
	}
}

func TestHelpCommandListsRegistry(t *testing.T) {
	r := NewRegistry()
	help := HelpCommand(r)
	mustRegister(t, r, help, Command{
		Name: "cost", Summary: "Show token and dollar accounting", Run: noop,
	})

	if help.Mode != FullScreen {
		t.Fatalf("help.Mode = %v, want FullScreen", help.Mode)
	}
	out, err := help.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("help.Run: %v", err)
	}
	for _, want := range []string{"/help", "/cost", "Show token and dollar accounting"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("help output missing %q:\n%s", want, out.Text)
		}
	}
}

func TestScriptabilityString(t *testing.T) {
	for s, want := range map[Scriptability]string{
		Both:            "both",
		Scriptable:      "scriptable",
		InteractiveOnly: "interactive-only",
	} {
		if got := s.String(); got != want {
			t.Errorf("Scriptability(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestParityTable(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r,
		Command{Name: "cost", Summary: "costs", Run: noop},
		Command{Name: "clear", Scriptability: InteractiveOnly, Reason: "fresh session each run", Run: noop},
	)
	table := ParityTable(r)
	for _, want := range []string{
		"| Command | Scriptability | Notes |",
		"| `/clear` | interactive-only | fresh session each run |",
		"| `/cost` | both |",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("ParityTable missing %q:\n%s", want, table)
		}
	}
}

// names extracts command names for terse assertions.
func names(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
