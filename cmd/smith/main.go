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
		fmt.Fprintf(os.Stderr, "smith: %v\n", err)
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
		printUsage(out)
		return nil
	}

	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	printUsage(out)
	return nil
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Agent Smith is a provider-agnostic coding agent harness.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  smith [--version]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Implementation status: scaffold only; agent runtime lands in later tickets.")
}
