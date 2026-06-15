// Package tui is Agent Smith's flagship interactive face (AS-021, PRD §7.8, D6):
// a streaming terminal chat over the agentic loop (AS-018), built on Bubble Tea
// (Model/Update/View) with Lipgloss styling and the stock Bubbles components for
// the input editor, scrollback, and spinner. Final assistant messages render as
// markdown via Glamour. See docs/design/adr-0001-tui-framework.md for the
// framework decision.
//
// The face is deliberately thin. It consumes only the loop's face-agnostic
// UIEvent stream and drives turns through the narrow Runner seam, so it imports
// neither internal/provider nor internal/tool — the vendor normalization and
// tool execution stay behind their interfaces (PRD §5, §9). A test
// (no_business_imports_test.go) enforces that boundary.
//
// The status line carries an always-visible context meter (AS-025): how full the
// current model's context window is and what the session has cost. It is fed by a
// MeterFunc the command wires up, so the face renders the gauge without importing
// the accounting engine — see meter.go.
//
// Slash commands (AS-022) plug in through internal/command: typing "/" opens a
// filterable palette over the registry handed to New, and dispatched commands
// render either inline (a transcript segment) or full-screen (a scrollable
// panel). The command framework is itself face-agnostic, so this face only
// renders the palette and routes keys — see palette.go.
//
// Inspect-mode panel host (AS-067): a full-screen command panel swaps over the
// transcript with the status line pinned (D-TUI-3); the leader chord Ctrl+G then
// a key opens common panels by name (D-TUI-4) without stealing bare-letter input
// (D-TUI-7). A reusable blocking modal overlay (modal.go) backs AS-024's
// destructive-permission prompts (D-TUI-8). A startup header (D-TUI-10) shows on
// launch unless WithoutSplash is set, and the status line degrades away first on
// a too-small terminal (D-TUI-11).
//
// Event flow: the loop calls its Observer inline on the goroutine driving a
// turn. App.Observer returns a loop.Observer that forwards each event onto a
// buffered channel; a long-lived tea.Cmd drains that channel into the Update
// loop. A turn runs in its own tea.Cmd whose context is cancelled when the user
// presses Esc, so cancellation is cooperative and the session stays usable.
package tui

import (
	"context"
	"errors"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/loop"
)

// eventBuffer bounds the channel that carries loop UIEvents into the program.
// The loop calls the Observer inline, so the buffer keeps a burst of deltas from
// blocking the turn goroutine between renders; the draining tea.Cmd keeps it
// flowing. A full buffer applies brief backpressure rather than dropping events.
const eventBuffer = 256

// Runner drives one user turn to completion, blocking until the loop stops or
// ctx is cancelled. It is satisfied by *loop.Engine. Declaring it here — rather
// than importing the engine's concrete type — is what keeps the face decoupled
// from the provider and tool packages the engine wires together.
type Runner interface {
	Run(ctx context.Context, userText string) (loop.Result, error)
}

// Meta is the session identity shown in the status line and startup header. The
// Matrix personality layer (AS-053) will dress this up; here it is plain text
// (D6).
type Meta struct {
	Provider string
	Model    string
	Session  string
	// Project labels the working context in the startup header (D-TUI-10); empty
	// is fine and is simply omitted.
	Project string
}

// MetaFunc yields the current session identity for the status line. It is
// re-read once per event/command (not per keystroke), so the provider, model,
// and session label stay current after a mid-session switch (/model) or a
// session swap (/clear, /resume) without the face importing the wiring that owns
// that state. A nil MetaFunc leaves the status-line identity empty.
type MetaFunc func() Meta

// App owns the Bubble Tea program and the bridge that carries loop events into
// it. Build it with New, hand Observer to the loop engine, then call Run.
type App struct {
	meta     MetaFunc
	events   chan loop.UIEvent
	commands *command.Registry
	meter    MeterFunc
	splash   bool

	// mu guards prog, which is set when Run starts the program. The permission
	// Asker (Ask) runs on the turn goroutine and reads prog to deliver a prompt
	// into the Update loop, so the two must synchronize.
	mu   sync.Mutex
	prog *tea.Program
}

// Option configures an App at construction. Options keep New's signature stable
// as toggles accrue (D2: additive).
type Option func(*App)

// WithoutSplash hides the startup header (D-TUI-10) — used by --no-splash and,
// once it lands, serious mode (AS-053).
func WithoutSplash() Option {
	return func(a *App) { a.splash = false }
}

// New builds an App for the given session-identity source and slash-command
// registry (commands may be nil to run without slash commands; meter may be nil
// to hide the context meter; meta may be nil for an empty status-line identity).
// The startup header shows by default; pass WithoutSplash to hide it. The
// returned App's Observer is usable immediately (so it can be wired into the
// engine before Run starts); events emitted before Run are simply buffered.
func New(meta MetaFunc, commands *command.Registry, meter MeterFunc, opts ...Option) *App {
	a := &App{
		meta:     meta,
		events:   make(chan loop.UIEvent, eventBuffer),
		commands: commands,
		meter:    meter,
		splash:   true,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Observer returns the loop.Observer that forwards UIEvents into the running UI.
// Register it on the engine (loop.WithObserver) so turn progress reaches the
// transcript. The send blocks only if the buffer is full, applying backpressure
// to the turn goroutine rather than dropping events.
func (a *App) Observer() loop.Observer {
	return func(ev loop.UIEvent) {
		a.events <- ev
	}
}

// Run starts the interactive program driving turns through runner, and blocks
// until the user quits. It uses the alternate screen and mouse support so
// scrollback and resize behave like a full-screen app.
func (a *App) Run(runner Runner) error {
	m := newModel(runner, a.meta, a.events, newMarkdownRenderer, a.commands, a.meter, a.splash)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	a.mu.Lock()
	a.prog = p
	a.mu.Unlock()
	_, err := p.Run()
	return err
}

// PermissionPrompt is a face-agnostic approval request the TUI renders for the
// permission model (AS-016/AS-024). Subject is the exact command or path being
// approved, shown verbatim — never a paraphrase (UX.md §11). Detail is optional
// extra verbatim context (a unified diff for an edit, AS-024 AC2). Destructive
// selects the surface (D-TUI-8): a broad-scope action escalates to a focus-trapped
// blocking modal, a normal one to an inline transcript card the user can scroll
// context behind before deciding.
type PermissionPrompt struct {
	Tool        string
	Subject     string
	Detail      string
	Destructive bool
}

// PermissionDecision is the user's answer to a PermissionPrompt. Allow gates this
// one call; Remember asks the policy to persist an allow-rule ("always allow").
type PermissionDecision struct {
	Allow    bool
	Remember bool
}

// Ask presents a permission prompt and blocks until the user decides or ctx is
// cancelled. It is the seam the loop's permission Asker (wired in the command)
// calls from the turn goroutine: it hands the prompt to the Update loop and waits
// for the decision on a private channel. A cancelled turn returns ctx.Err(), which
// the policy treats as a denial, so an aborted turn never hangs a tool call.
func (a *App) Ask(ctx context.Context, p PermissionPrompt) (PermissionDecision, error) {
	a.mu.Lock()
	prog := a.prog
	a.mu.Unlock()
	if prog == nil {
		return PermissionDecision{}, errors.New("tui: not running")
	}
	reply := make(chan PermissionDecision, 1)
	prog.Send(permPromptMsg{prompt: p, reply: reply})
	select {
	case d := <-reply:
		return d, nil
	case <-ctx.Done():
		return PermissionDecision{}, ctx.Err()
	}
}
