package main

import "github.com/tonitienda/agent-smith/internal/command"

// parityDoc renders docs/project/command-parity.md from the shared command
// registry (AS-066), so the slash ↔ subcommand parity matrix (UX.md §17.5) is
// generated, never hand-maintained. TestCommandParityDocInSync keeps the
// checked-in file equal to this output; regenerate with
// `UPDATE_DOCS=1 go test ./cmd/smith`.
func parityDoc() string {
	return "# Command parity\n\n" +
		"<!-- Generated from the command registry by cmd/smith (AS-066). Do not edit by hand;\n" +
		"regenerate with `UPDATE_DOCS=1 go test ./cmd/smith`. -->\n\n" +
		"Per [UX.md §17.5](../UX.md) every built-in command declares whether it is\n" +
		"interactive-only, scriptable, or both, sourced from the one descriptor each\n" +
		"command registers — so the TUI palette, the CLI `--help`, and this table can't\n" +
		"disagree. Interactive-only commands state a reason.\n\n" +
		command.ParityTable(chatCommands(nil)) + "\n"
}
