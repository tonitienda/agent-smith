package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

// newTestApp builds an App over captured buffers with a small command tree:
// a leaf `echo` that emits its first arg (honoring --output), a leaf `boom`
// that fails at runtime, and a `session list|resume` noun group.
func newTestApp(stdinTTY, stdoutTTY bool) (*App, *bytes.Buffer, *bytes.Buffer, *bool) {
	var out, errb bytes.Buffer
	bareRan := false
	app := &App{
		Name:      "smith",
		Tagline:   "test harness",
		Version:   "smith 0.0.0-test",
		Stdin:     strings.NewReader(""),
		Stdout:    &out,
		Stderr:    &errb,
		StdinTTY:  stdinTTY,
		StdoutTTY: stdoutTTY,
		Getenv:    func(string) string { return "" },
		Bare:      func(*Context) error { bareRan = true; return nil },
		Commands: []*Command{
			{
				Name:    "echo",
				Summary: "echo the first argument",
				Usage:   "<text>",
				Examples: []string{
					"smith echo hi",
				},
				Run: func(c *Context) error {
					arg := ""
					if len(c.Args) > 0 {
						arg = c.Args[0]
					}
					return c.Emit(arg)
				},
			},
			{
				Name:    "boom",
				Summary: "always fails",
				Run:     func(*Context) error { return errFail },
			},
			{
				Name:    "session",
				Summary: "manage sessions",
				Sub: []*Command{
					{Name: "list", Summary: "list sessions", Run: func(c *Context) error { return c.Emit("listed") }},
					{Name: "resume", Summary: "resume a session", Run: func(c *Context) error { return c.Emit("resumed") }},
				},
			},
		},
	}
	return app, &out, &errb, &bareRan
}

var errFail = &runtimeErr{}

type runtimeErr struct{}

func (*runtimeErr) Error() string { return "kaboom" }

func TestBareLaunchesTUIOnTTY(t *testing.T) {
	app, _, _, bareRan := newTestApp(true, true)
	if code := app.Run(nil); code != ExitOK {
		t.Fatalf("bare on TTY exit = %d, want %d", code, ExitOK)
	}
	if !*bareRan {
		t.Fatal("bare handler did not run on a TTY")
	}
}

func TestBareNonTTYIsUsageError(t *testing.T) {
	app, out, errb, bareRan := newTestApp(false, false)
	if code := app.Run(nil); code != ExitUsage {
		t.Fatalf("bare off TTY exit = %d, want %d", code, ExitUsage)
	}
	if *bareRan {
		t.Fatal("bare handler ran off a TTY")
	}
	if out.Len() != 0 {
		t.Errorf("usage went to stdout: %q", out.String())
	}
	if !strings.Contains(errb.String(), "Usage:") {
		t.Errorf("stderr missing usage:\n%s", errb.String())
	}
}

func TestVersionAndHelp(t *testing.T) {
	app, out, _, _ := newTestApp(true, true)
	if code := app.Run([]string{"--version"}); code != ExitOK {
		t.Fatalf("--version exit = %d", code)
	}
	if !strings.Contains(out.String(), "0.0.0-test") {
		t.Errorf("--version output = %q", out.String())
	}

	app, out, _, _ = newTestApp(true, true)
	if code := app.Run([]string{"--help"}); code != ExitOK {
		t.Fatalf("--help exit = %d", code)
	}
	for _, want := range []string{"Commands:", "echo", "session", "Global flags:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--help missing %q:\n%s", want, out.String())
		}
	}
}

func TestUnknownCommandSuggests(t *testing.T) {
	app, _, errb, _ := newTestApp(true, true)
	code := app.Run([]string{"sesson"}) // typo of "session"
	if code != ExitUsage {
		t.Fatalf("unknown command exit = %d, want %d", code, ExitUsage)
	}
	got := errb.String()
	if !strings.Contains(got, "unknown command") {
		t.Errorf("stderr missing unknown-command line:\n%s", got)
	}
	if !strings.Contains(got, `Did you mean "session"?`) {
		t.Errorf("stderr missing suggestion:\n%s", got)
	}
}

func TestLeafEmitsToStdout(t *testing.T) {
	app, out, _, _ := newTestApp(false, false)
	if code := app.Run([]string{"echo", "hello"}); code != ExitOK {
		t.Fatalf("echo exit = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "hello" {
		t.Errorf("echo output = %q, want hello", got)
	}
}

func TestOutputJSONWrapsResult(t *testing.T) {
	app, out, _, _ := newTestApp(false, false)
	if code := app.Run([]string{"echo", "hi", "--output", "json"}); code != ExitOK {
		t.Fatalf("echo --output json exit = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != `{"text":"hi"}` {
		t.Errorf("json output = %q", got)
	}
}

func TestInvalidOutputIsUsageError(t *testing.T) {
	app, _, errb, _ := newTestApp(false, false)
	if code := app.Run([]string{"echo", "hi", "--output", "yaml"}); code != ExitUsage {
		t.Fatalf("bad --output exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errb.String(), "invalid --output") {
		t.Errorf("stderr missing reason:\n%s", errb.String())
	}
}

func TestRuntimeFailureExitsOne(t *testing.T) {
	app, _, errb, _ := newTestApp(false, false)
	if code := app.Run([]string{"boom"}); code != ExitFail {
		t.Fatalf("boom exit = %d, want %d", code, ExitFail)
	}
	if !strings.Contains(errb.String(), "kaboom") {
		t.Errorf("stderr missing error:\n%s", errb.String())
	}
}

func TestNounGroupDispatch(t *testing.T) {
	app, out, _, _ := newTestApp(false, false)
	if code := app.Run([]string{"session", "list"}); code != ExitOK {
		t.Fatalf("session list exit = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "listed" {
		t.Errorf("session list output = %q", got)
	}
}

func TestNounGroupNoVerbIsUsageError(t *testing.T) {
	app, out, errb, _ := newTestApp(false, false)
	if code := app.Run([]string{"session"}); code != ExitUsage {
		t.Fatalf("session (no verb) exit = %d, want %d", code, ExitUsage)
	}
	if out.Len() != 0 {
		t.Errorf("group help went to stdout: %q", out.String())
	}
	if !strings.Contains(errb.String(), "resume") {
		t.Errorf("group help missing verbs:\n%s", errb.String())
	}
}

func TestNounGroupUnknownVerbSuggests(t *testing.T) {
	app, _, errb, _ := newTestApp(false, false)
	if code := app.Run([]string{"session", "resme"}); code != ExitUsage {
		t.Fatalf("bad verb exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errb.String(), `Did you mean "session resume"?`) {
		t.Errorf("stderr missing scoped suggestion:\n%s", errb.String())
	}
}

func TestCommandHelpJSON(t *testing.T) {
	app, out, _, _ := newTestApp(false, false)
	if code := app.Run([]string{"echo", "--help", "--output", "json"}); code != ExitOK {
		t.Fatalf("echo --help --output json exit = %d", code)
	}
	got := out.String()
	for _, want := range []string{`"name": "echo"`, `"usage": "<text>"`, `"examples"`} {
		if !strings.Contains(got, want) {
			t.Errorf("help json missing %q:\n%s", want, got)
		}
	}
}

func TestCommandHelpTextHasExamples(t *testing.T) {
	app, out, _, _ := newTestApp(false, false)
	if code := app.Run([]string{"echo", "--help"}); code != ExitOK {
		t.Fatalf("echo --help exit = %d", code)
	}
	for _, want := range []string{"Usage:", "smith echo hi", "Global flags:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help text missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommandSpecificFlag(t *testing.T) {
	var got string
	app, _, _, _ := newTestApp(false, false)
	app.Commands = append(app.Commands, &Command{
		Name:  "tag",
		Flags: func(fs *flag.FlagSet) { fs.StringVar(&got, "name", "", "a name") },
		Run:   func(c *Context) error { return nil },
	})
	if code := app.Run([]string{"tag", "--name", "x"}); code != ExitOK {
		t.Fatalf("tag exit = %d", code)
	}
	if got != "x" {
		t.Errorf("command flag --name = %q, want x", got)
	}
}

func TestCommandHelpShowsCommandSpecificFlags(t *testing.T) {
	app, out, _, _ := newTestApp(false, false)
	app.Commands = append(app.Commands, &Command{
		Name:    "tag",
		Summary: "tag something",
		Flags: func(fs *flag.FlagSet) {
			fs.String("name", "", "a name")
			fs.Bool("force", false, "skip confirmation")
		},
		Run: func(*Context) error { return nil },
	})

	// Text help lists the command flags in their own block, distinct from globals.
	if code := app.Run([]string{"tag", "--help"}); code != ExitOK {
		t.Fatalf("tag --help exit = %d", code)
	}
	text := out.String()
	for _, want := range []string{"\nFlags:\n", "--name", "a name", "--force", "skip confirmation", "Global flags:"} {
		if !strings.Contains(text, want) {
			t.Errorf("text help missing %q:\n%s", want, text)
		}
	}
	// The globals must not be duplicated into the command block.
	if strings.Count(text, "--output") != 1 {
		t.Errorf("globals duplicated into command flags:\n%s", text)
	}

	// JSON help carries the same flags.
	out.Reset()
	if code := app.Run([]string{"tag", "--help", "--output", "json"}); code != ExitOK {
		t.Fatalf("tag --help --output json exit = %d", code)
	}
	js := out.String()
	for _, want := range []string{`"flags"`, `"name": "name"`, `"name": "force"`, `"default": "false"`} {
		if !strings.Contains(js, want) {
			t.Errorf("json help missing %q:\n%s", want, js)
		}
	}
}

func TestColorAutoRespectsTTYAndNoColor(t *testing.T) {
	// Off a TTY → no color even on auto.
	app, _, _, _ := newTestApp(false, false)
	var seen Globals
	app.Commands = []*Command{{Name: "c", Run: func(c *Context) error { seen = c.Globals; return nil }}}
	app.Run([]string{"c"})
	if seen.UseColor {
		t.Error("auto color on a non-TTY, want off")
	}

	// On a TTY with NO_COLOR set → still off.
	app, _, _, _ = newTestApp(true, true)
	app.Getenv = func(k string) string {
		if k == "NO_COLOR" {
			return "1"
		}
		return ""
	}
	app.Commands = []*Command{{Name: "c", Run: func(c *Context) error { seen = c.Globals; return nil }}}
	app.Run([]string{"c"})
	if seen.UseColor {
		t.Error("NO_COLOR ignored on a TTY, want color off")
	}

	// --color always forces it on even off a TTY.
	app, _, _, _ = newTestApp(false, false)
	app.Commands = []*Command{{Name: "c", Run: func(c *Context) error { seen = c.Globals; return nil }}}
	app.Run([]string{"c", "--color", "always"})
	if !seen.UseColor {
		t.Error("--color always did not force color on")
	}
}
