package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/openai"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/internal/tui"
	"github.com/tonitienda/agent-smith/internal/version"
)

// defaultModel is the model a fresh session starts on; /model switches it
// mid-session (AS-023) and routing will pick it per turn later (AS-042). Override
// the starting model with SMITH_MODEL.
const defaultModel = "claude-opus-4-8"

// isTerminal reports whether f is attached to a terminal, used for TTY detection
// (the bare-invocation TUI launch and output auto-detection, D-CLI-2/D-CLI-4).
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// startChat wires the substrate — a persisted session log, the configured
// providers, the built-in file and shell tools, and the agentic loop — behind
// the TUI face and runs it. A non-empty resumeID resumes that session instead of
// starting a fresh one (AS-023 /resume; `smith --resume <id>`); noSplash hides
// the startup header (AS-067; `smith --no-splash`). The mutable session state
// (active provider/model, current log) lives in chatSession, which the TUI drives
// through the Runner/Meta/Meter seams, so all of this provider/tool wiring stays
// here in the command, never in internal/tui.
func startChat(resumeID string, noSplash bool) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	store, err := session.NewStore("", wd)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	sess, err := openOrCreate(store, resumeID)
	if err != nil {
		return err
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

	pricing, err := cost.Default()
	if err != nil {
		return fmt.Errorf("load pricing table: %w", err)
	}

	providers := map[string]provider.Provider{
		"anthropic": anthropic.New(""),
		"openai":    openai.New(""),
	}
	// A resumed session keeps the model it last used so its window/cost meter
	// matches; otherwise start on the configured default (Anthropic). The model is
	// adopted only when its provider is configured, so the provider and model never
	// disagree (a model with no provider would fail at turn time).
	provName, model := "anthropic", chatModel()
	if m := lastModel(sess.Log.Events()); m != "" {
		if r, ok := pricing.Lookup(m); ok && r.Vendor != "" {
			if _, ok := providers[r.Vendor]; ok {
				provName, model = r.Vendor, m
			}
		}
	}

	ctl := newChatSession(store, reg, pricing, providers, sess, provName, model, filepath.Base(wd))
	var opts []tui.Option
	if noSplash {
		opts = append(opts, tui.WithoutSplash())
	}
	app := tui.New(ctl.Meta, chatCommands(ctl), ctl.Meter, opts...)
	// Wire the permission gate before the first engine is built, so every tool
	// call is approved through the TUI (AS-016/AS-024). The Asker delivers prompts
	// into the running app, which app.Run starts below.
	policy, err := buildPolicy(wd, app)
	if err != nil {
		return fmt.Errorf("load permission policy: %w", err)
	}
	ctl.setPolicy(policy)
	if err := ctl.start(app.Observer()); err != nil {
		return fmt.Errorf("build engine: %w", err)
	}

	return app.Run(ctl)
}

// openOrCreate resumes the session named by resumeID, or creates a fresh one when
// resumeID is empty.
func openOrCreate(store *session.Store, resumeID string) (*session.Session, error) {
	if resumeID != "" {
		sess, err := store.Open(resumeID)
		if err != nil {
			return nil, fmt.Errorf("resume session %q: %w", resumeID, err)
		}
		return sess, nil
	}
	sess, err := store.Create("")
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

// chatCommands builds the slash-command registry for the chat face. It ships
// /help (a full-screen list of every registered command), /version (inline),
// /cost (AS-020, a full-screen token & dollar breakdown), /context (AS-026, the
// full-screen window-composition view), and the parity power commands /clear,
// /model, and /resume (AS-023), and the wedge command /clean (AS-028). The
// handlers close over the chat controller so the command package stays
// dependency-free.
func chatCommands(ctl *chatSession) *command.Registry {
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
		Name:          "cost",
		Summary:       "Show token & cost accounting for the session",
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{"smith cost", "smith cost --output json"},
		Run: func(context.Context, []string) (command.Output, error) {
			summary := cost.Summarize(ctl.events(), ctl.pricing)
			return command.Output{Text: cost.Render(summary)}, nil
		},
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "context",
		Summary:       "Show what's filling the context window",
		Args:          "[size|age|type]",
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{"smith context show", "smith context show age"},
		Run:           ctl.cmdContext,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "clean",
		Summary:       "Remove segments from the context window",
		Args:          "<handle>… | --apply | --undo | --cancel",
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Run:           ctl.cmdClean,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "clear",
		Summary:       "Start a fresh session (the old one stays in /resume)",
		Mode:          command.Inline,
		Scriptability: command.InteractiveOnly,
		Reason:        "clears the active session in place; a headless run is already a fresh session, so there is nothing to clear",
		Run:           ctl.cmdClear,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "model",
		Summary:       "List models, or switch the active model",
		Args:          "[id]",
		Mode:          command.Inline,
		Scriptability: command.Both,
		Run:           ctl.cmdModel,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "resume",
		Summary:       "List recent sessions, or load one by ID",
		Args:          "[id]",
		Mode:          command.Inline,
		Scriptability: command.Both,
		Examples:      []string{"smith session list", "smith session resume <id>"},
		Run:           ctl.cmdResume,
	})
	return reg
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
