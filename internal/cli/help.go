package cli

import (
	"flag"
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
	b.WriteString(commandFlagsHelp(cmd))
	b.WriteString("\nGlobal flags:\n")
	b.WriteString(globalFlagsHelp())
	return b.String()
}

// helpEntry is the machine-readable registry entry `--help --output json` dumps
// (D-CLI-10), so tooling and docs read the same source the palette does. For
// shared verbs every field is sourced from the command.Registry descriptor
// (AS-066), so the JSON help can't drift from the slash command. Fields are
// additive (D2): output schema fills in per-command as commands grow one (AS-051).
type helpEntry struct {
	Name          string   `json:"name"`
	Summary       string   `json:"summary"`
	Usage         string   `json:"usage"`
	Scriptability string   `json:"scriptability,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	OutputSchema  string   `json:"outputSchema,omitempty"`
	Examples      []string `json:"examples,omitempty"`
	// Flags lists the command-specific flags (globals are documented once, on the
	// root help — they're not repeated per command, AS-070).
	Flags []flagEntry `json:"flags,omitempty"`
}

// flagEntry is one command-specific flag in `--help --output json`. Name is the
// bare flag (no dashes); Default is the stringified zero value, omitted when empty.
type flagEntry struct {
	Name    string `json:"name"`
	Usage   string `json:"usage"`
	Default string `json:"default,omitempty"`
}

// writeCommandHelpJSON emits the command's registry entry as JSON to stdout.
func (a *App) writeCommandHelpJSON(cmd *Command, path string) error {
	entry := helpEntry{
		Name:          strings.TrimSpace(path),
		Summary:       cmd.Summary,
		Usage:         cmd.Usage,
		Scriptability: cmd.Scriptability,
		Reason:        cmd.Reason,
		OutputSchema:  cmd.OutputSchema,
		Examples:      cmd.Examples,
	}
	for _, f := range commandFlags(cmd) {
		_, usage := flag.UnquoteUsage(f)
		entry.Flags = append(entry.Flags, flagEntry{Name: f.Name, Usage: usage, Default: f.DefValue})
	}
	return writeJSON(a.Stdout, entry, "  ")
}

// commandFlags collects a leaf's command-specific flags by registering them on a
// throwaway FlagSet — the globals (registerGlobals) are documented separately, so
// only the command's own Flags are visited here.
func commandFlags(cmd *Command) []*flag.Flag {
	if cmd.Flags == nil {
		return nil
	}
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	cmd.Flags(fs)
	var out []*flag.Flag
	fs.VisitAll(func(f *flag.Flag) { out = append(out, f) })
	return out
}

// commandFlagsHelp renders the command-specific flags block, or "" when the
// command has none. A single-rune flag prints as -f, longer names as --resume.
func commandFlagsHelp(cmd *Command) string {
	flags := commandFlags(cmd)
	if len(flags) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nFlags:\n")
	for _, f := range flags {
		name, usage := flag.UnquoteUsage(f)
		left := flagDash(f.Name)
		if name != "" {
			left += " " + name
		}
		fmt.Fprintf(&b, "  %-33s %s\n", left, usage)
	}
	return b.String()
}

// flagDash prefixes a flag name with the dash count its length implies: -h for a
// single rune, --help otherwise.
func flagDash(name string) string {
	if len([]rune(name)) == 1 {
		return "-" + name
	}
	return "--" + name
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
	return "  --output plain|json|stream-json   result format (default: plain)\n" +
		"  --color auto|always|never         color (honors NO_COLOR; default auto)\n" +
		"  -q, --quiet / -v, --verbose       tune stderr diagnostics\n" +
		"  --config <path>                   config file (overrides the default chain)\n" +
		"  --yes                             confirm destructive ops on a non-TTY\n" +
		"  -h, --help                        show help\n"
}
