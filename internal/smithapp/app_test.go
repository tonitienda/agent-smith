package smithapp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cli"
)

func TestBuildCLIWiresRouterShellWithoutStartingFaces(t *testing.T) {
	var out, errb bytes.Buffer
	bareCalled := false
	app := BuildCLI(CLIConfig{
		Stdin:     strings.NewReader(""),
		Stdout:    &out,
		Stderr:    &errb,
		StdinTTY:  true,
		StdoutTTY: true,
		Version:   "smith test",
		Bare: func(*cli.Context) error {
			bareCalled = true
			return nil
		},
		Commands: []*cli.Command{{Name: "noop", Summary: "No-op", Run: func(*cli.Context) error { return nil }}},
	})

	if app.Name != "smith" || app.Version != "smith test" {
		t.Fatalf("unexpected app identity: name=%q version=%q", app.Name, app.Version)
	}
	if code := app.Run([]string{"noop"}); code != cli.ExitOK {
		t.Fatalf("noop exit = %d, stderr=%s", code, errb.String())
	}
	if bareCalled {
		t.Fatal("subcommand dispatch unexpectedly launched the bare TUI handler")
	}
}

func TestBuildCLIBareHandlerIsInjected(t *testing.T) {
	bareCalled := false
	app := BuildCLI(CLIConfig{
		Stdin:     strings.NewReader(""),
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		StdinTTY:  true,
		StdoutTTY: true,
		Bare: func(*cli.Context) error {
			bareCalled = true
			return nil
		},
	})

	if code := app.Run(nil); code != cli.ExitOK {
		t.Fatalf("bare exit = %d", code)
	}
	if !bareCalled {
		t.Fatal("bare handler was not called")
	}
}
