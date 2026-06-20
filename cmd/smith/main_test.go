package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cli"
)

// writeTemp writes content to a fresh temp file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// testApp builds the production command tree over captured buffers, so the tests
// exercise the real router wiring without launching the TUI or touching argv.
func testApp(stdinTTY, stdoutTTY bool) (*cli.App, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &cli.App{
		Name:      "smith",
		Tagline:   "Agent Smith is a provider-agnostic coding agent harness.",
		Version:   "smith test",
		Stdin:     strings.NewReader(""),
		Stdout:    &out,
		Stderr:    &errb,
		StdinTTY:  stdinTTY,
		StdoutTTY: stdoutTTY,
		Getenv:    func(string) string { return "" },
		Bare:      func(*cli.Context) error { return nil },
		Commands:  commands(),
	}, &out, &errb
}

// TestCLIVerbsShareRegistryHandlers guards D-CLI-10 / AC#6: the registry names
// the CLI verbs dispatch to (cost, context, resume) must exist in the same
// command.Registry the TUI palette renders, so a subcommand and its slash command
// run identical code rather than a forked copy.
func TestCLIVerbsShareRegistryHandlers(t *testing.T) {
	reg := chatCommands(newTestController(t))
	for _, name := range []string{"cost", "context", "resume"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("registry is missing %q — a CLI verb dispatches to it", name)
		}
	}
}

func TestVersion(t *testing.T) {
	app, out, _ := testApp(true, true)
	if code := app.Run([]string{"--version"}); code != cli.ExitOK {
		t.Fatalf("--version exit = %d", code)
	}
	if !strings.Contains(out.String(), "smith test") {
		t.Fatalf("version output = %q", out.String())
	}
}

func TestHelpListsNounGroups(t *testing.T) {
	app, out, _ := testApp(true, true)
	if code := app.Run([]string{"--help"}); code != cli.ExitOK {
		t.Fatalf("--help exit = %d", code)
	}
	for _, want := range []string{"run", "session", "context", "cost", "config"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--help missing command %q:\n%s", want, out.String())
		}
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	app, _, errb := testApp(true, true)
	if code := app.Run([]string{"contex"}); code != cli.ExitUsage {
		t.Fatalf("unknown command exit = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errb.String(), `Did you mean "context"?`) {
		t.Errorf("stderr missing suggestion:\n%s", errb.String())
	}
}

func TestRunWithoutPromptIsUsageError(t *testing.T) {
	// stdin is a TTY and no positional/-f → no prompt, usage error (exit 2).
	app, _, errb := testApp(true, true)
	if code := app.Run([]string{"run"}); code != cli.ExitUsage {
		t.Fatalf("run with no prompt exit = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errb.String(), "no prompt") {
		t.Errorf("stderr missing reason:\n%s", errb.String())
	}
}

func TestRunHelpJSONDumpsRegistryEntry(t *testing.T) {
	app, out, _ := testApp(false, false)
	if code := app.Run([]string{"run", "--help", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("run --help --output json exit = %d", code)
	}
	for _, want := range []string{`"name": "run"`, `"usage": "<prompt>"`, `"examples"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help json missing %q:\n%s", want, out.String())
		}
	}
}

func TestConfigGetRespectsConfigFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explicit.json")
	if err := os.WriteFile(path, []byte(`{"model":"from-explicit"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app, out, _ := testApp(false, false)
	if code := app.Run([]string{"config", "get", "model", "--config", path}); code != cli.ExitOK {
		t.Fatalf("config get --config exit = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "from-explicit" {
		t.Errorf("config get --config = %q, want from-explicit", got)
	}
}

func TestConfigSetQuietWritesWithoutDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	app, _, errb := testApp(false, false)
	if code := app.Run([]string{"config", "set", "model", "gpt-4o", "--config", path, "--quiet"}); code != cli.ExitOK {
		t.Fatalf("config set --quiet exit = %d", code)
	}
	if errb.Len() != 0 {
		t.Errorf("--quiet still wrote to stderr: %q", errb.String())
	}
	// The value round-trips through `config get` over the same explicit file.
	app2, out, _ := testApp(false, false)
	if code := app2.Run([]string{"config", "get", "model", "--config", path}); code != cli.ExitOK {
		t.Fatalf("config get after set exit = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "gpt-4o" {
		t.Errorf("config set then get = %q, want gpt-4o", got)
	}
}

func TestResolvePromptSources(t *testing.T) {
	cases := []struct {
		name    string
		ctx     *cli.Context
		file    string
		want    string
		wantErr bool
	}{
		{
			name: "positional",
			ctx:  &cli.Context{Args: []string{"fix", "the", "test"}, StdinTTY: true},
			want: "fix the test",
		},
		{
			name: "explicit dash reads stdin",
			ctx:  &cli.Context{Args: []string{"-"}, Stdin: strings.NewReader("piped task\n"), StdinTTY: true},
			want: "piped task",
		},
		{
			name: "piped stdin when no args",
			ctx:  &cli.Context{Stdin: strings.NewReader("from pipe"), StdinTTY: false},
			want: "from pipe",
		},
		{
			name:    "no prompt on a TTY is an error",
			ctx:     &cli.Context{StdinTTY: true},
			wantErr: true,
		},
		{
			name: "non-TTY with empty stdin falls back to -f",
			ctx:  &cli.Context{Stdin: strings.NewReader(""), StdinTTY: false},
			file: writeTemp(t, "from file\n"),
			want: "from file",
		},
		{
			name: "piped stdin beats -f",
			ctx:  &cli.Context{Stdin: strings.NewReader("from pipe"), StdinTTY: false},
			file: writeTemp(t, "from file\n"),
			want: "from pipe",
		},
		{
			name:    "empty -f file is a usage error",
			ctx:     &cli.Context{Stdin: strings.NewReader(""), StdinTTY: false},
			file:    writeTemp(t, "   \n"),
			wantErr: true,
		},
		{
			name:    "blank piped stdin beats -f and errors",
			ctx:     &cli.Context{Stdin: strings.NewReader("\n"), StdinTTY: false},
			file:    writeTemp(t, "from file\n"),
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePrompt(tc.ctx, tc.file)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolvePrompt = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePrompt: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolvePrompt = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestArityParityAcrossFaces asserts the CLI face enforces the same descriptor
// arity the slash face does (internal/tui TestCommandArityRejectedBeforeRun):
// an over-arity subcommand is a usage error before any session is opened, and
// `session resume` with no id is rejected just like the slash command needs an
// argument to load. The bound is read from the shared command.Registry, so the
// two faces can't disagree about valid argument counts (AS-090).
func TestArityParityAcrossFaces(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"cost rejects extra", []string{"cost", "extra"}, "takes no arguments"},
		{"session resume rejects two", []string{"session", "resume", "a", "b"}, "takes at most 1 argument"},
		{"session resume needs id", []string{"session", "resume"}, "needs at least 1 argument"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _, errb := testApp(true, true)
			if code := app.Run(tc.args); code != cli.ExitUsage {
				t.Fatalf("%v exit = %d, want %d (stderr: %s)", tc.args, code, cli.ExitUsage, errb.String())
			}
			if !strings.Contains(errb.String(), tc.want) {
				t.Errorf("stderr = %q, want containing %q", errb.String(), tc.want)
			}
		})
	}
}
