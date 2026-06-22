package cli

import (
	"flag"
	"fmt"
	"reflect"
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
	// Sub carries a noun group's verbs so `--help --output json` exposes the whole
	// command tree; empty for leaf commands (additive, D2).
	Sub []helpEntry `json:"sub,omitempty"`
}

// rootHelpEntry is what `smith --help --output json` dumps: the root summary, the
// global flags, and the command tree, so tooling discovers the same surface the
// text help shows (AS-116). Fields are additive (D2).
type rootHelpEntry struct {
	Name        string      `json:"name"`
	Summary     string      `json:"summary,omitempty"`
	Usage       string      `json:"usage"`
	Version     string      `json:"version,omitempty"`
	GlobalFlags []flagEntry `json:"globalFlags"`
	Commands    []helpEntry `json:"commands"`
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
	return writeJSON(a.Stdout, commandHelpEntry(cmd, path), "  ")
}

// commandHelpEntry builds the JSON help entry for a command, recursing into a
// noun group's verbs so the whole tree is described from one node.
func commandHelpEntry(cmd *Command, path string) helpEntry {
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
	for _, sub := range cmd.Sub {
		entry.Sub = append(entry.Sub, commandHelpEntry(sub, path+" "+sub.Name))
	}
	return entry
}

// writeRootHelpJSON emits the root help as JSON: tagline, usage, global flags, and
// the command tree, so `smith --help --output json` is machine-readable (AS-116).
func (a *App) writeRootHelpJSON() error {
	entry := rootHelpEntry{
		Name:        a.Name,
		Summary:     a.Tagline,
		Usage:       a.Name + " [command] [flags]",
		Version:     a.Version,
		GlobalFlags: globalFlagEntries(),
		Commands:    []helpEntry{},
	}
	for _, c := range a.Commands {
		entry.Commands = append(entry.Commands, commandHelpEntry(c, c.Name))
	}
	return writeJSON(a.Stdout, entry, "  ")
}

// globalFlagEntries lists the shared flags for the root JSON help, skipping the
// single-rune aliases (-q/-v/-h) since they duplicate their long forms.
func globalFlagEntries() []flagEntry {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	registerGlobals(fs)
	out := []flagEntry{}
	fs.VisitAll(func(f *flag.Flag) {
		if len([]rune(f.Name)) == 1 {
			return
		}
		_, usage := flag.UnquoteUsage(f)
		out = append(out, flagEntry{Name: f.Name, Usage: usage, Default: f.DefValue})
	})
	return out
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
		// Show a meaningful default, matching the global flags block; a zero value
		// adds no information so it stays implicit.
		if !isZeroFlag(f) {
			usage += fmt.Sprintf(" (default: %s)", f.DefValue)
		}
		fmt.Fprintf(&b, "  %-33s %s\n", left, usage)
	}
	return b.String()
}

// isZeroFlag reports whether a flag's default is the zero value for its type, so
// help omits an uninformative "(default: …)". It mirrors the standard library's
// own (unexported) flag.isZeroValue: build a fresh zero of the flag.Value's type
// and compare its String() to the recorded default, so every type is handled by
// its own formatting — e.g. a time.Duration zero is "0s", not "0" — instead of a
// hardcoded list of zero strings.
func isZeroFlag(f *flag.Flag) bool {
	typ := reflect.TypeOf(f.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Pointer {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	return f.DefValue == z.Interface().(flag.Value).String()
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
