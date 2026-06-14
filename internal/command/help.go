package command

import (
	"context"
	"fmt"
	"strings"
)

// HelpCommand returns a `/help` command that lists every command registered in
// r, so any face gets help for free by registering it. It renders full-screen
// because the list can outgrow a few inline lines. r is read when the command
// runs, so commands registered after HelpCommand still appear.
func HelpCommand(r *Registry) Command {
	return Command{
		Name:    "help",
		Summary: "List available commands",
		Mode:    FullScreen,
		Run: func(_ context.Context, _ []string) (Output, error) {
			return Output{Text: renderHelp(r)}, nil
		},
	}
}

// renderHelp formats the registry as an aligned "name  args — summary" list.
func renderHelp(r *Registry) string {
	cmds := r.All()
	if len(cmds) == 0 {
		return "No commands available."
	}

	// Width of the "/name args" column, so summaries line up.
	invocations := make([]string, len(cmds))
	width := 0
	for i, c := range cmds {
		inv := "/" + c.Name
		if c.Args != "" {
			inv += " " + c.Args
		}
		invocations[i] = inv
		if len(inv) > width {
			width = len(inv)
		}
	}

	var b strings.Builder
	b.WriteString("Commands\n\n")
	for i, c := range cmds {
		line := fmt.Sprintf("  %-*s", width, invocations[i])
		if c.Summary != "" {
			line += "  " + c.Summary
		}
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
