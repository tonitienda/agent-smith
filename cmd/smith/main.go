// Command smith is the Agent Smith CLI entrypoint.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tonitienda/agent-smith/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if _, printErr := fmt.Fprintf(os.Stderr, "smith: %v\n", err); printErr != nil {
			os.Exit(1)
		}
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("smith", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version and exit")
	showHelp := flags.Bool("help", false, "print help and exit")
	flags.BoolVar(showHelp, "h", false, "print help and exit")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		_, err := fmt.Fprintln(out, version.String())
		return err
	}

	if *showHelp {
		return printUsage(out)
	}

	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	// No flags: launch the interactive chat face when attached to a terminal.
	// Off a TTY (scripts, CI, `make test`) fall back to usage so the binary stays
	// well-behaved in non-interactive contexts.
	if !interactiveTerminal() {
		return printUsage(out)
	}
	return startChat()
}

func printUsage(out io.Writer) error {
	lines := []string{
		"Agent Smith is a provider-agnostic coding agent harness.",
		"",
		"Usage:",
		"  smith            start an interactive chat session (requires a terminal)",
		"  smith --version  print version and exit",
		"  smith --help     print this help and exit",
		"",
		"Set ANTHROPIC_API_KEY to talk to the Anthropic provider.",
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}
