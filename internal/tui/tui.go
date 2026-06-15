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
// Event flow: the loop calls its Observer inline on the goroutine driving a
// turn. App.Observer returns a loop.Observer that forwards each event onto a
// buffered channel; a long-lived tea.Cmd drains that channel into the Update
// loop. A turn runs in its own tea.Cmd whose context is cancelled when the user
// presses Esc, so cancellation is cooperative and the session stays usable.
package tui

import (
	"context"

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

// Meta is the static session identity shown in the status line. The Matrix
// personality layer (AS-053) will dress this up; here it is plain text (D6).
type Meta struct {
	Provider string
	Model    string
	Session  string
}

// App owns the Bubble Tea program and the bridge that carries loop events into
// it. Build it with New, hand Observer to the loop engine, then call Run.
type App struct {
	meta     Meta
	events   chan loop.UIEvent
	commands *command.Registry
	meter    MeterFunc
}

// New builds an App for the given session metadata and slash-command registry
// (commands may be nil to run without slash commands; meter may be nil to hide
// the context meter). The returned App's Observer is usable immediately (so it
// can be wired into the engine before Run starts); events emitted before Run are
// simply buffered.
func New(meta Meta, commands *command.Registry, meter MeterFunc) *App {
	return &App{
		meta:     meta,
		events:   make(chan loop.UIEvent, eventBuffer),
		commands: commands,
		meter:    meter,
	}
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
	m := newModel(runner, a.meta, a.events, newMarkdownRenderer, a.commands, a.meter)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
