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
	"sync"
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
	// Picker, when non-nil, asks an interactive face to present a single-select
	// list and, on choice, re-dispatch this same command with the chosen item's
	// Value as its sole argument (AS-064 /resume picker). A non-interactive face
	// (headless/CLI) ignores it and renders Text instead, so a handler returns
	// both: the scriptable listing in Text and the interactive list in Picker.
	// Advisory and additive (D2).
	Picker *Picker
	// Selector, when non-nil, asks an interactive face to present a multi-select
	// list with a live preview and a confirm action (AS-068 /clean in-panel
	// selection). The handler supplies the selectable items, the archived items
	// that can be restored one at a time, and the closures that compute the live
	// preview and commit a selection — so the face drives the selection without
	// importing the engine behind it (mirrors how handlers close over session
	// state). A non-interactive face ignores it and renders Text instead. Advisory
	// and additive (D2).
	Selector *Selector
	// Prompt, when non-empty, asks the face to submit this text as a fresh user
	// turn — the expansion of a custom slash command (AS-033), whose whole purpose
	// is to feed a templated prompt to the model rather than print to the
	// transcript. A face that drives turns runs it like typed input; one that
	// cannot ignores it. Advisory and additive (D2); a handler setting Prompt
	// normally leaves Text empty.
	Prompt string
}

// Picker is an interactive single-select list a Handler offers to interactive
// faces (AS-064). Choosing an item re-runs the originating command with the
// item's Value as its only argument, so the picker is pure sugar over the
// command's existing argument form (e.g. /resume <id>) — no new handler path.
type Picker struct {
	// Title labels the selection list.
	Title string
	// Items are the choices in display order.
	Items []PickerItem
}

// PickerItem is one choice in a Picker: Label is the one-line display text and
// Value is the argument handed back to the command when the item is chosen.
type PickerItem struct {
	Label string
	Value string
}

// Selector is an interactive multi-select surface a Handler offers to
// interactive faces (AS-068): the user toggles a selection over Items, sees the
// running reclaim preview, and confirms to apply — and can restore an Archive
// item one at a time. The engine work stays in the closures (the handler closes
// over the session), so the face holds only data and functions and never imports
// the projection/clean packages (the AS-021 boundary).
type Selector struct {
	// Title labels the selection surface.
	Title string
	// Items are the selectable rows in display order (e.g. the live /context
	// segments, largest first).
	Items []SelectItem
	// Archive are rows that can be restored individually rather than selected
	// (e.g. blocks already excluded from the window). May be empty.
	Archive []SelectItem
	// Preview computes the live reclaim summary for the currently selected item
	// Values; the face calls it as the selection changes. It must be pure (no log
	// mutation). A nil Preview yields no live preview.
	Preview func(values []string) SelectPreview
	// Apply commits the selected item Values as one removal and returns a result
	// line for the face to surface. A nil Apply makes the surface read-only.
	Apply func(values []string) string
	// Restore re-includes a single Archive item by Value and returns a result
	// line. A nil Restore disables per-item restore.
	Restore func(value string) string
}

// SelectItem is one row in a Selector: Label is the one-line display text and
// Value is the key handed back to Preview/Apply/Restore (e.g. a block handle).
type SelectItem struct {
	Label string
	Value string
}

// SelectPreview is the live feedback a Selector shows for the current selection:
// a one-line Summary (count and tokens/$ reclaimed) and any soft Warnings.
type SelectPreview struct {
	Summary  string
	Warnings []string
}

// Handler runs a command with its parsed arguments and returns what to render.
// ctx carries cancellation from the face; args are post-parse positional
// tokens (quotes already stripped), never including the command name.
type Handler func(ctx context.Context, args []string) (Output, error)

// ArgSpec is a command's declarative positional-argument arity contract (AS-090).
// Faces parse their own surface — the TUI lexes a slash line (Parse), the CLI
// permutes flags ahead of positionals — then hand the resulting positional args
// to CheckArity, so both reject the same out-of-range argument counts before the
// shared Handler runs. Min is the smallest valid count; Max the largest, with a
// negative Max meaning unbounded.
type ArgSpec struct {
	Min int
	Max int
}

// CheckArity validates args against the command's ArgSpec. A nil ArgSpec accepts
// any count (arity is unchecked). The error names the command so the same
// message reaches whichever face surfaced the call.
func (c Command) CheckArity(args []string) error {
	s := c.ArgSpec
	if s == nil {
		return nil
	}
	if len(args) < s.Min {
		return fmt.Errorf("/%s: needs %s, got %d", c.Name, atLeast(s.Min), len(args))
	}
	if s.Max >= 0 && len(args) > s.Max {
		return fmt.Errorf("/%s: takes %s, got %d", c.Name, atMost(s.Max), len(args))
	}
	return nil
}

// atLeast and atMost render an arity bound for CheckArity's diagnostics.
func atLeast(n int) string { return fmt.Sprintf("at least %s", plural(n, "argument")) }
func atMost(n int) string {
	if n == 0 {
		return "no arguments"
	}
	return fmt.Sprintf("at most %s", plural(n, "argument"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Command is one registered slash command.
type Command struct {
	// Name is the invocation token without the leading slash (e.g. "clean").
	Name string
	// Summary is the one-line description shown in the palette and `/help`.
	Summary string
	// Args is a human-readable argument spec for help, e.g. `"<topic>"` or
	// `[name]`. It is documentation only; ArgSpec enforces arity.
	Args string
	// ArgSpec, when non-nil, is the positional-argument arity contract every
	// face checks before Run (AS-090): a slash command and its subcommand can't
	// disagree about how many arguments are valid, because both read it from this
	// one descriptor. A nil ArgSpec leaves arity unchecked (the backward-compatible
	// default, D2), so a command opts into validation by setting it.
	ArgSpec *ArgSpec
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
// NewRegistry. Access is guarded by mu because custom slash commands (AS-033) are
// rescanned and Upserted at runtime (as the palette opens) while command handlers
// — e.g. /help's renderHelp calling All — read the registry from their own
// goroutine; the lock keeps those concurrent map accesses safe.
type Registry struct {
	mu     sync.RWMutex
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
	if err := validate(c); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.byName[c.Name]; dup {
		return fmt.Errorf("command %q: already registered", c.Name)
	}
	r.byName[c.Name] = c
	return nil
}

// Upsert adds c, replacing any command already registered under the same name.
// Unlike Register it tolerates a clobber: it is the entry point for the custom
// slash commands (AS-033), which are rediscovered from disk on palette open and
// must be allowed to replace their prior version when their file changes. It
// applies the same name/handler validation as Register.
func (r *Registry) Upsert(c Command) error {
	if err := validate(c); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[c.Name] = c
	return nil
}

// validate enforces the rules shared by Register and Upsert: a non-empty name
// with no leading slash or whitespace, a non-nil handler, and a Reason on any
// interactive-only command.
func validate(c Command) error {
	switch {
	case c.Name == "":
		return fmt.Errorf("command: empty name")
	case strings.HasPrefix(c.Name, "/"):
		return fmt.Errorf("command %q: name must not include the leading slash", c.Name)
	case strings.ContainsAny(c.Name, " \t\n"):
		return fmt.Errorf("command %q: name must not contain whitespace", c.Name)
	case c.Run == nil:
		return fmt.Errorf("command %q: nil handler", c.Name)
	case c.Scriptability == InteractiveOnly && strings.TrimSpace(c.Reason) == "":
		// UX.md §17.5: an interactive-only command must say why it can't be
		// scripted, so the parity table never lists a silent interactive-only one.
		return fmt.Errorf("command %q: interactive-only command must state a Reason", c.Name)
	}
	return nil
}

// Lookup returns the command registered under name (without the slash).
func (r *Registry) Lookup(name string) (Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byName[name]
	return c, ok
}

// All returns every command sorted by name, for `/help` and an unfiltered
// palette.
func (r *Registry) All() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.all()
}

// all returns every command sorted by name. The caller must hold at least a read
// lock; it exists so lock-holding methods (All, Match) share one body without
// re-acquiring the non-reentrant RWMutex.
func (r *Registry) all() []Command {
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if query == "" {
		return r.all()
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
	r.mu.RLock()
	names := r.names()
	r.mu.RUnlock()
	return nearest(strings.ToLower(name), names)
}

// Nearest returns the candidate closest to name, for a "did you mean …?" hint
// over an arbitrary name set (e.g. the CLI router's subcommands, AS-065). ok is
// false when nothing is close enough. It shares the registry's scoring so the
// slash palette and the CLI suggest identically.
func Nearest(name string, candidates []string) (string, bool) {
	return nearest(strings.ToLower(name), candidates)
}

// names returns the registered command names (unsorted). The caller must hold at
// least a read lock.
func (r *Registry) names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}
