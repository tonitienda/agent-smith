package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/internal/tui"
	"github.com/tonitienda/agent-smith/internal/version"
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

	pricing, err := cost.Default()
	if err != nil {
		return fmt.Errorf("load pricing table: %w", err)
	}

	app := tui.New(tui.Meta{
		Provider: prov.Name(),
		Model:    model,
		Session:  shortID(sess.ID),
	}, chatCommands(sess.Log, pricing), chatMeter(sess.Log, pricing))
	engine, err := loop.New(prov, sess.Log, runtime, reg, model, loop.WithObserver(app.Observer()))
	if err != nil {
		return fmt.Errorf("build engine: %w", err)
	}

	return app.Run(engine)
}

// chatCommands builds the slash-command registry for the chat face. It ships
// /help (a full-screen list of every registered command), /version (inline), and
// /cost (AS-020) — a full-screen token & dollar breakdown derived from the
// session log. The remaining commands (/context, /clean, /model, /resume,
// /clear) arrive in their own tickets (AS-023, AS-026, AS-028). The cost handler
// closes over the session log and pricing table so the command package stays
// dependency-free.
func chatCommands(log *eventlog.Log, pricing *cost.Table) *command.Registry {
	reg := command.NewRegistry()
	// HelpCommand reads the registry lazily, so it lists commands registered after
	// it too; registering it first is fine.
	mustRegisterCommand(reg, command.HelpCommand(reg))
	mustRegisterCommand(reg, command.Command{
		Name:    "version",
		Summary: "Show the Agent Smith version",
		Mode:    command.Inline,
		Run: func(context.Context, []string) (command.Output, error) {
			return command.Output{Text: version.String()}, nil
		},
	})
	mustRegisterCommand(reg, command.Command{
		Name:    "cost",
		Summary: "Show token & cost accounting for this session",
		Mode:    command.FullScreen,
		Run: func(context.Context, []string) (command.Output, error) {
			summary := cost.Summarize(log.Events(), pricing)
			return command.Output{Text: cost.Render(summary)}, nil
		},
	})
	return reg
}

// chatMeter builds the context-meter snapshot function for the chat status line
// (AS-025). It closes over the session log and pricing table and takes the active
// model per call, so the window denominator rescales the moment the model is
// switched (AS-023 /model) while internal/tui stays decoupled from the accounting
// engine. It derives live window occupancy and session cost from the same log the
// /cost command reads — one accounting source, no drift. Window occupancy is the
// most recent turn's prompt+output tokens (the figure the provider last counted),
// refined later by per-block estimates (AS-063); the denominator is the model's
// context window from the pricing table.
//
// The TUI calls this on every loop event, including each streamed text delta, but
// the log only grows between turns — so the result is memoized on the log length
// and active model and recomputed only when one of them changes, keeping the
// per-delta cost a single O(1) length check rather than a full re-summarize. The
// closure is called only from the Bubble Tea goroutine, so its memo state needs
// no locking.
func chatMeter(log *eventlog.Log, pricing *cost.Table) tui.MeterFunc {
	lastLen := -1
	var lastModel string
	var cached tui.Meter
	return func(model string) tui.Meter {
		if n := log.Len(); n == lastLen && model == lastModel {
			return cached
		}
		summary := cost.Summarize(log.Events(), pricing)
		used := 0
		if last, ok := summary.Latest(); ok {
			used = last.ContextTokens()
		}
		window, _ := pricing.Window(model)
		cached = tui.Meter{
			Tokens:    used,
			Window:    window,
			CostUSD:   summary.TotalUSD,
			CostKnown: summary.AllPriced,
		}
		lastLen, lastModel = log.Len(), model
		return cached
	}
}

// mustRegisterCommand registers a built-in command, panicking on error. The
// built-ins are static, so a registration failure is a programming bug that
// should surface immediately at startup, not be silently dropped.
func mustRegisterCommand(reg *command.Registry, c command.Command) {
	if err := reg.Register(c); err != nil {
		panic(fmt.Sprintf("register built-in command %q: %v", c.Name, err))
	}
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
