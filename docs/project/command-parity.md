# Command parity

<!-- Generated from the command registry by cmd/smith (AS-066). Do not edit by hand;
regenerate with `UPDATE_DOCS=1 go test ./cmd/smith`. -->

Per [UX.md §17.5](../UX.md) every built-in command declares whether it is
interactive-only, scriptable, or both, sourced from the one descriptor each
command registers — so the TUI palette, the CLI `--help`, and this table can't
disagree. Interactive-only commands state a reason.

| Command | Scriptability | Notes |
|---|---|---|
| `/budget` | both |  |
| `/clean` | both |  |
| `/clear` | interactive-only | clears the active session in place; a headless run is already a fresh session, so there is nothing to clear |
| `/compact` | both |  |
| `/context` | both |  |
| `/cost` | both |  |
| `/goal` | both |  |
| `/help` | both |  |
| `/model` | both |  |
| `/resume` | both |  |
| `/rewind` | both |  |
| `/serious` | interactive-only | mutes/restores interactive chrome flavor; non-interactive faces are already clean |
| `/version` | both |  |
