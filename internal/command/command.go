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

// Output is what a Handler produces for a face to render. It is intentionally
// plain text for V1; richer payloads (tables, diffs) are additive later (D2).
type Output struct {
	Text string
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

// names returns the registered command names (unsorted).
func (r *Registry) names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}
