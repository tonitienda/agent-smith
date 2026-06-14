package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/internal/tui"
)

// defaultModel is the model issued for interactive turns until model selection
// (AS-023 /model, AS-042 routing) lands. Override with SMITH_MODEL.
const defaultModel = "claude-opus-4-8"

// interactiveTerminal reports whether stdin and stdout are both a terminal, so
// the full-screen chat face can run.
func interactiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// startChat wires the substrate — a persisted session log, the Anthropic
// provider, the built-in file and shell tools, and the agentic loop — behind the
// TUI face and runs it. The face consumes only the loop's UIEvents, so all of
// this provider/tool wiring stays here in the command, never in internal/tui.
func startChat() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	store, err := session.NewStore("", wd)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	sess, err := store.Create("")
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	reg := tool.NewRegistry()
	fs, err := builtin.NewFS(wd)
	if err != nil {
		return fmt.Errorf("init file tools: %w", err)
	}
	for _, t := range fs.Tools() {
		if err := reg.Register(t); err != nil {
			return fmt.Errorf("register tool: %w", err)
		}
	}
	shell, err := builtin.NewShell(wd)
	if err != nil {
		return fmt.Errorf("init shell tool: %w", err)
	}
	if err := reg.Register(shell); err != nil {
		return fmt.Errorf("register shell tool: %w", err)
	}

	runtime := tool.NewRuntime(reg, sess.Log)
	prov := anthropic.New("")
	model := chatModel()

	app := tui.New(tui.Meta{
		Provider: prov.Name(),
		Model:    model,
		Session:  shortID(sess.ID),
	})
	engine, err := loop.New(prov, sess.Log, runtime, reg, model, loop.WithObserver(app.Observer()))
	if err != nil {
		return fmt.Errorf("build engine: %w", err)
	}

	return app.Run(engine)
}

// chatModel returns the model ID for interactive turns, honoring SMITH_MODEL.
func chatModel() string {
	if m := strings.TrimSpace(os.Getenv("SMITH_MODEL")); m != "" {
		return m
	}
	return defaultModel
}

// shortID trims a session ID to a compact status-line label.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
