// Command smith is the Agent Smith CLI entrypoint. It builds the face-neutral
// subcommand router (AS-065, internal/cli) and dispatches argv to it; bare
// `smith` on a terminal drops into the TUI (D-CLI-2).
package main

import (
	"os"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/smithapp"
)

var appRuntime = smithapp.NewRuntime(smithapp.RuntimeConfig{})

func main() {
	os.Exit(buildApp().Run(os.Args[1:]))
}

// buildApp assembles the router: the IO streams, TTY detection, the verb tree,
// and the bare-invocation TUI launch.
func buildApp() *cli.App {
	return smithapp.BuildCLI(smithapp.CLIConfig{
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		StdinTTY:  isTerminal(os.Stdin),
		StdoutTTY: isTerminal(os.Stdout),
		Getenv:    os.Getenv,
		Bare:      bareTUI,
		Commands:  commands(),
	})
}
