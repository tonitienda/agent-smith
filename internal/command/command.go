// Package command is Agent Smith's face-agnostic slash-command framework
// (AS-022, PRD §7.6, §7.8, D6). A Registry holds the built-in commands
// (`/cost`, `/context`, `/clean`, `/model`, …) plus, eventually, user-defined
// ones (fast-follow, D6); faces query it to drive a command palette, `/help`,
// nearest-match suggestions, and dispatch.
//
// The package deliberately knows nothing about any face or about the
// provider/tool layers: a Command is a name, a little metadata, and a Handler.
// The TUI (internal/tui) imports it to render the palette and dispatch keys;
// the headless CLI (AS-051) can reuse the same registry. Handlers that need
// session, loop, or cost state close over it when they are registered by the
// command wiring, keeping this package dependency-free.
package command

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Mode is how a face should render a command's output. Commands declare it up
// front (not per invocation) so a palette can hint the behavior before running.
type Mode int

const (
	// Inline output is appended to the running transcript, like a chat reply.
	Inline Mode = iota
	// FullScreen output replaces the transcript with a scrollable panel until
	// dismissed (e.g. `/help`, the `/context` composition view).
	FullScreen
)

// Scriptability says which faces a command is meant for (UX.md §9.3, §17.5): the
// interactive TUI, the headless/CLI face, or both. A face and the parity table
// (AS-066) read it from the one descriptor, so the slash command and its CLI
// subcommand can't disagree about whether the command can be scripted. The zero
// value is Both, the common case.
type Scriptability int

const (
	// Both runs interactively and when scripted (the default, zero value).
	Both Scriptability = iota
	// Scriptable is headless/CLI only.
	Scriptable
	// InteractiveOnly is TUI only; it must carry a Reason (UX.md §17.5).
	InteractiveOnly
)

// String renders the scriptability for help, JSON, and the parity table.
func (s Scriptability) String() string {
	switch s {
	case Scriptable:
		return "scriptable"
	case InteractiveOnly:
		return "interactive-only"
	default:
		return "both"
	}
}

// Output is what a Handler produces for a face to render. It is intentionally
// plain text for V1; richer payloads (tables, diffs) are additive later (D2).
type Output struct {
	Text string
	// ResetView asks the face to clear its transcript/scrollback because the
	// command started or restored a different session (e.g. /clear, /resume), so
	// the view reflects the fresh context rather than the previous session's.
	// It is advisory and additive (D2): a face with no transcript may ignore it.
	ResetView bool
}

// Handler runs a command with its parsed arguments and returns what to render.
// ctx carries cancellation from the face; args are post-parse positional
// tokens (quotes already stripped), never including the command name.
type Handler func(ctx context.Context, args []string) (Output, error)

// Command is one registered slash command.
type Command struct {
	// Name is the invocation token without the leading slash (e.g. "clean").
	Name string
	// Summary is the one-line description shown in the palette and `/help`.
	Summary string
	// Args is a human-readable argument spec for help, e.g. `"<topic>"` or
	// `[name]`. It is documentation only; parsing does not enforce it.
	Args string
	// Mode is how the output renders (Inline or FullScreen).
	Mode Mode
	// Scriptability declares which faces the command serves (UX.md §17.5). The
	// zero value is Both; an InteractiveOnly command must set Reason.
	Scriptability Scriptability
	// Reason explains why an InteractiveOnly command can't be scripted; it is
	// required for InteractiveOnly and ignored otherwise (UX.md §17.5).
	Reason string
	// Examples are runnable invocations shown in help (D-CLI-10). They are
	// face-neutral text shared by every face that renders examples; the CLI
	// router reads them so a verb's `--help` and the slash command agree.
	Examples []string
	// OutputSchema names the per-command structured output emitted under
	// `--output json`, where one exists beyond the shared `{text}` envelope
	// (internal/cli Emit). Empty today for every built-in — richer per-command
	// schemas are additive in AS-051 (D2) — but exposed now so help and the
	// parity table surface it the moment a command grows one.
	OutputSchema string
	// Run executes the command. A nil Run is rejected at registration.
	Run Handler
}

// Registry holds commands by name. The zero value is not usable; build one with
// NewRegistry. It is not safe for concurrent mutation, which is fine: commands
// are registered once at startup, then only read.
type Registry struct {
	byName map[string]Command
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Command)}
}

// Register adds c to the registry. It rejects an empty name, a name containing
// whitespace or a leading slash, a nil handler, and duplicate names, so the
// "registering a command makes it appear everywhere" contract can't be violated
// by a malformed entry.
func (r *Registry) Register(c Command) error {
	name := c.Name
	switch {
	case name == "":
		return fmt.Errorf("command: empty name")
	case strings.HasPrefix(name, "/"):
		return fmt.Errorf("command %q: name must not include the leading slash", name)
	case strings.ContainsAny(name, " \t\n"):
		return fmt.Errorf("command %q: name must not contain whitespace", name)
	case c.Run == nil:
		return fmt.Errorf("command %q: nil handler", name)
	case c.Scriptability == InteractiveOnly && strings.TrimSpace(c.Reason) == "":
		// UX.md §17.5: an interactive-only command must say why it can't be
		// scripted, so the parity table never lists a silent interactive-only one.
		return fmt.Errorf("command %q: interactive-only command must state a Reason", name)
	}
	if _, dup := r.byName[name]; dup {
		return fmt.Errorf("command %q: already registered", name)
	}
	r.byName[name] = c
	return nil
}

// Lookup returns the command registered under name (without the slash).
func (r *Registry) Lookup(name string) (Command, bool) {
	c, ok := r.byName[name]
	return c, ok
}

// All returns every command sorted by name, for `/help` and an unfiltered
// palette.
func (r *Registry) All() []Command {
	out := make([]Command, 0, len(r.byName))
	for _, c := range r.byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Match returns the commands whose names fuzzy-match query, best first. An empty
// query returns everything (sorted by name), so typing just "/" lists all
// commands. See match.go for the scoring.
func (r *Registry) Match(query string) []Command {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return r.All()
	}
	type scored struct {
		cmd   Command
		score int
	}
	var hits []scored
	for _, c := range r.byName {
		if s, ok := fuzzyScore(query, c.Name); ok {
			hits = append(hits, scored{cmd: c, score: s})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score // higher score first
		}
		return hits[i].cmd.Name < hits[j].cmd.Name // stable tiebreak
	})
	out := make([]Command, len(hits))
	for i, h := range hits {
		out[i] = h.cmd
	}
	return out
}

// Suggest returns the registered name closest to an unknown one, for the
// "did you mean …?" hint. ok is false when nothing is close enough.
func (r *Registry) Suggest(name string) (string, bool) {
	return nearest(strings.ToLower(name), r.names())
}

// Nearest returns the candidate closest to name, for a "did you mean …?" hint
// over an arbitrary name set (e.g. the CLI router's subcommands, AS-065). ok is
// false when nothing is close enough. It shares the registry's scoring so the
// slash palette and the CLI suggest identically.
func Nearest(name string, candidates []string) (string, bool) {
	return nearest(strings.ToLower(name), candidates)
}

// names returns the registered command names (unsorted).
func (r *Registry) names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}
