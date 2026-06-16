package cli

import (
	"fmt"
	"strings"
)

// rootHelp renders the top-level usage: the tagline, the command list, and the
// global flags. clig.dev: help on the root, examples everywhere (D-CLI-10).
func (a *App) rootHelp() string {
	var b strings.Builder
	if a.Tagline != "" {
		b.WriteString(a.Tagline)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Usage:\n  %s [command] [flags]\n  %s                 start the interactive TUI (on a terminal)\n\n", a.Name, a.Name)
	b.WriteString("Commands:\n")
	writeCommandList(&b, a.Commands)
	b.WriteString("\nGlobal flags:\n")
	b.WriteString(globalFlagsHelp())
	fmt.Fprintf(&b, "\nRun '%s <command> --help' for details on a command.\n", a.Name)
	return b.String()
}

// groupHelp renders a noun group's verbs (e.g. `session list|resume`).
func (a *App) groupHelp(cmd *Command, path string) string {
	var b strings.Builder
	if cmd.Summary != "" {
		b.WriteString(cmd.Summary)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Usage:\n  %s %s <command> [flags]\n\nCommands:\n", a.Name, path)
	writeCommandList(&b, cmd.Sub)
	return b.String()
}

// commandHelp renders a leaf command's help: usage, summary, and examples.
func (a *App) commandHelp(cmd *Command, path string) string {
	var b strings.Builder
	usage := path
	if cmd.Usage != "" {
		usage += " " + cmd.Usage
	}
	fmt.Fprintf(&b, "Usage:\n  %s %s [flags]\n", a.Name, usage)
	if cmd.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", cmd.Summary)
	}
	if len(cmd.Examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, ex := range cmd.Examples {
			fmt.Fprintf(&b, "  %s\n", ex)
		}
	}
	b.WriteString("\nGlobal flags:\n")
	b.WriteString(globalFlagsHelp())
	return b.String()
}

// helpEntry is the machine-readable registry entry `--help --output json` dumps
// (D-CLI-10), so tooling and docs read the same source the palette does. Fields
// are additive (D2): scriptability and output schema join in AS-066/AS-051.
type helpEntry struct {
	Name     string   `json:"name"`
	Summary  string   `json:"summary"`
	Usage    string   `json:"usage"`
	Examples []string `json:"examples,omitempty"`
}

// writeCommandHelpJSON emits the command's registry entry as JSON to stdout.
func (a *App) writeCommandHelpJSON(cmd *Command, path string) error {
	entry := helpEntry{
		Name:     strings.TrimSpace(path),
		Summary:  cmd.Summary,
		Usage:    cmd.Usage,
		Examples: cmd.Examples,
	}
	return writeJSON(a.Stdout, entry, "  ")
}

// writeCommandList prints an aligned "name  summary" block for a command set.
func writeCommandList(b *strings.Builder, cmds []*Command) {
	width := 0
	for _, c := range cmds {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range cmds {
		line := fmt.Sprintf("  %-*s", width, c.Name)
		if c.Summary != "" {
			line += "  " + c.Summary
		}
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteByte('\n')
	}
}

// globalFlagsHelp is the static block describing the shared flags.
func globalFlagsHelp() string {
	return "  --output plain|json|stream-json   result format (default: auto by TTY)\n" +
		"  --color auto|always|never         color (honors NO_COLOR; default auto)\n" +
		"  -q, --quiet / -v, --verbose       tune stderr diagnostics\n" +
		"  --config <path>                   config file (overrides the default chain)\n" +
		"  --yes                             confirm destructive ops on a non-TTY\n" +
		"  -h, --help                        show help\n"
}
