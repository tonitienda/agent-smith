package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/compact"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/customcmd"
	"github.com/tonitienda/agent-smith/internal/delegate"
	"github.com/tonitienda/agent-smith/internal/memory"
	"github.com/tonitienda/agent-smith/internal/personality"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/smithapp"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/internal/tui"
	"github.com/tonitienda/agent-smith/internal/version"
)

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
func startChat(resumeID string, noSplash bool, override string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	store, err := session.NewStore("", wd)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	sess, err := smithapp.OpenOrCreate(store, resumeID)
	if err != nil {
		return err
	}
	// Load the project's memory files into a fresh session only (AS-032). A
	// resumed session already carries the memory blocks it was created with, so
	// re-seeding would duplicate them; mid-session refresh is out of scope.
	if resumeID == "" {
		if err := seedMemory(wd, sess); err != nil {
			return err
		}
	}
	// Scan portable skills once (AS-034); the same snapshot builds the skill tool
	// below and seeds the skill_load events, so the offered catalog and the logged
	// events can't diverge. seedSkills reconciles (deduped), so it is safe on a
	// fresh, cleared, or resumed session alike.
	skills, err := skill.Load(skill.UserDir(), skill.ProjectDir(wd))
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	if err := seedSkills(sess, skills); err != nil {
		return err
	}
	debugLog, err := openDebugLog(sess.Dir)
	if err != nil {
		return err
	}
	defer func() { _ = debugLog.Close() }()
	debugLog.Printf("interactive session start id=%s project=%q cwd=%q", sess.ID, wd, wd)

	reg, err := appRuntime.BuiltinTools(wd)
	if err != nil {
		return err
	}
	if err := registerSkillTool(reg, skills); err != nil {
		return err
	}

	pricing, err := sessionPricing(override)
	if err != nil {
		return fmt.Errorf("load pricing table: %w", err)
	}

	// Load the lifecycle-hook set (AS-035) from the layered config; a load error
	// or a malformed spec degrades to no hooks rather than aborting the session.
	cfg, err := loadLayeredConfig(override)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hooks := loadHooks(cfg, os.Stderr)
	// Capture-time redaction (AS-115): off by default, scrubs high-confidence
	// secrets before they reach the log when `redaction.enabled` is set.
	applyRedaction(cfg, sess.Log, os.Stderr)

	// Connect configured MCP servers (AS-036) and register their tools; a server
	// that fails to connect is skipped, never fatal. Clients are closed at session
	// end to reap any subprocesses.
	mcpClients := connectMCPServers(context.Background(), cfg, reg, os.Stderr)
	defer closeMCPClients(mcpClients)

	providers := wrapProvidersWithDebugLog(appRuntime.Providers(), debugLog)
	// A resumed session keeps the model it last used so its window/cost meter
	// matches; otherwise start on the configured default (Anthropic). The model is
	// adopted only when its provider is configured, so the provider and model never
	// disagree (a model with no provider would fail at turn time).
	model := appRuntime.ChatModel()
	if m := lastModel(sess.Log.Events()); m != "" {
		model = m
	}
	provName, model := appRuntime.SelectProviderModel(pricing, providers, model)

	ctl := newChatSession(store, reg, pricing, providers, sess, provName, model, wd, skills, hooks)
	// Apply the configured budget defaults (AS-041): a default ceiling for new
	// sessions and the warning fraction, both overridable per session by /budget.
	// The typed view (AS-093) owns the `budget.*` paths and validates them.
	budgetCfg, budgetWarns := budget.ConfigFrom(cfg)
	for _, w := range budgetWarns {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	ctl.setBudgetDefaults(budgetCfg.DefaultLimitUSD, budgetCfg.WarnFraction, budgetCfg.HaltUnpriced)
	// Auto-compact (AS-085) is off by default — the product prefers /clean and
	// /tidy; this is the blunt last-resort guard against a context-window-exceeded
	// stop. The typed view (AS-093) owns the `compact.*` paths and defaults the
	// trigger threshold.
	compactCfg, compactWarns := compact.ConfigFrom(cfg)
	for _, w := range compactWarns {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	ctl.setAutoCompact(compactCfg.Auto, compactCfg.AutoThreshold)
	// Model routing/tiering (AS-042): the tier→model policy that /compact
	// summarization (and future tier-declaring features) resolve through, plus
	// per-feature overrides. The typed view (AS-093) owns the `routing.*` paths;
	// a malformed section degrades to the default policy with a warning (D2).
	routingCfg, routingWarns := routing.ConfigFrom(cfg)
	for _, w := range routingWarns {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	ctl.setRouter(routingCfg)
	// Wire the system sub-agents (AS-107): register the built-in passive analyzers
	// (AS-048) with the `subagents.<name>` config overlay, and hand the controller
	// the registry plus the insights store findings record into (the /insights seam,
	// AS-045). Default-on costs nothing when idle; a malformed entry warns, not fatal.
	// Inject the durable fact ledger and the memory/skill-aware save-target
	// resolver (AS-108) so a dismissed fact stays dismissed across sessions and a
	// fact found inside a skill scope proposes saving to that skill.
	subReg, subStore, err := buildSubAgents(cfg, store, saveTargetResolver(wd, skills), openFactLedger(store, os.Stderr), skills, os.Stderr)
	if err != nil {
		return fmt.Errorf("build sub-agents: %w", err)
	}
	ctl.setSubAgents(subReg, subStore)
	// Build the Matrix personality layer (AS-053) from config for this interactive
	// face: themed status line + role names, on by default in the TUI, with
	// /serious as the runtime kill switch. It is chrome-only and never touches turn
	// behavior or output. Must be set before chatCommands so /serious shares it.
	var persSettings personality.Settings
	if _, err := cfg.Decode("personality", &persSettings); err != nil {
		// A malformed personality section degrades to the plain defaults rather than
		// aborting the session, but is surfaced so a syntax/type error is diagnosable.
		fmt.Fprintf(os.Stderr, "warning: ignoring personality config: %v\n", err)
	}
	ctl.setPersonality(personality.New(persSettings, true))
	// Build the slash-command registry, then layer custom commands (AS-033) over
	// the built-ins. rescanCustom re-reads them so a file dropped into the commands
	// dir becomes invocable without a restart; the TUI runs it as the palette opens.
	cmds := chatCommands(ctl)
	// Layer MCP commands (AS-083): /mcp health+reconnect, and one command per server
	// prompt. Registered before builtinNames so custom commands can't clobber them.
	registerMCPCommands(cmds, mcpClients, os.Stderr)
	builtins := builtinNames(cmds)
	rescanCustom := func() { registerCustomCommands(cmds, builtins, wd) }
	rescanCustom()
	opts := []tui.Option{tui.WithRehydrate(ctl.rehydrate), tui.WithCommandRefresh(rescanCustom), tui.WithWorkingLine(ctl.workingLine)}
	if noSplash {
		opts = append(opts, tui.WithoutSplash())
	}
	app := tui.New(ctl.Meta, cmds, ctl.Meter, opts...)
	// Wire the permission gate before the first engine is built, so every tool
	// call is approved through the TUI (AS-016/AS-024). The Asker delivers prompts
	// into the running app, which app.Run starts below.
	policy, err := buildPolicy(wd, app, override)
	if err != nil {
		return fmt.Errorf("load permission policy: %w", err)
	}
	ctl.setPolicy(policy)
	// User-delegated subagents (AS-046, PRD §7.17): register the `task` tool, which
	// delegates a scoped prompt to a child agent running its own isolated, persisted
	// session (linked to this one) and summarizes the result back into this context.
	// The child reuses the parent's provider and permission gate and the cheap
	// routing tier for fan-out; its tool set inherits the parent's skills (AS-034)
	// and live MCP tools (AS-036) but omits `task`, so delegation does not recurse
	// (AS-119). Registered before start so the first engine offers it, and after
	// setPolicy so the child inherits the gate.
	spawner := taskSpawner(store, wd, skills, mcpClients, func() delegate.Parent {
		ctl.mu.Lock()
		defer ctl.mu.Unlock()
		// policy is set by setPolicy above for the interactive face, but guard
		// the nil case (a face that skips the gate) rather than dereference it.
		var perm tool.PermissionFunc
		if ctl.policy != nil {
			perm = ctl.policy.Func()
		}
		return delegate.Parent{
			Log:                ctl.sess.Log,
			SessionID:          ctl.sess.ID,
			ProvName:           ctl.provName,
			Model:              ctl.model,
			Permission:         perm,
			Router:             ctl.router,
			Pricing:            pricing,
			ChildBudgetUSD:     budgetCfg.PerChildLimitUSD,
			BudgetWarnFraction: budgetCfg.WarnFraction,
		}
	})
	if err := reg.Register(builtin.NewTask(spawner)); err != nil {
		return fmt.Errorf("register task tool: %w", err)
	}
	if err := ctl.start(app.Observer()); err != nil {
		return fmt.Errorf("build engine: %w", err)
	}
	// Fire the session-stop hook (AS-035) as the app shuts down, with the final
	// log in place.
	defer ctl.stop()

	return app.Run(ctl)
}

// seedMemory loads the project's memory files (AGENTS.md / AGENT.md / CLAUDE.md,
// AS-032) discovered from wd and appends them to a freshly created session's log
// as model-facing memory blocks, so the first turn sees them and /context
// attributes them to their source. It is called only for new sessions; a resumed
// session already carries the blocks it was created with.
func seedMemory(wd string, sess *session.Session) error {
	blocks, err := memory.Load(memory.UserDir(), wd)
	if err != nil {
		return fmt.Errorf("load memory files: %w", err)
	}
	for _, b := range blocks {
		if _, err := sess.Log.Append(b); err != nil {
			return fmt.Errorf("append memory block: %w", err)
		}
	}
	return nil
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
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 0},
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
		Name:          "route",
		Summary:       "Inspect or override the model routing/tiering policy",
		Args:          "[<feature> <tier> | <tier> <vendor> <model>]",
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 3},
		Examples:      []string{"smith route", "smith route compact standard", "smith route cheap anthropic claude-haiku-4-5"},
		Run:           ctl.cmdRoute,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "insights",
		Summary:       "Session retrospective: measured signals + suggestions",
		Args:          "[apply <n>]",
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 2},
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{"smith insights", "smith insights apply 1"},
		Run:           ctl.cmdInsights,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "skills",
		Summary:       "Living-skills report: per-session findings + cross-session rollup",
		Args:          "[apply <n>]",
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 2},
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{"smith skills", "smith skills apply 1"},
		Run:           ctl.cmdSkills,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "clean",
		Summary:       "Remove segments from the context window",
		Args:          "<handle>… | \"<topic>\" | --apply | --undo | --cancel",
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("apply", false, "confirm the staged removal")
			fs.Bool("undo", false, "restore the most recent removal")
			fs.Bool("cancel", false, "discard the staged preview")
		},
		Run: ctl.cmdClean,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "tidy",
		Summary:       "Dedupe repeated file reads in the context window",
		Args:          "[--apply | --undo | --cancel]",
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 0},
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{"smith tidy", "smith tidy --apply"},
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("apply", false, "confirm the staged dedup")
			fs.Bool("undo", false, "restore the most recent dedup")
			fs.Bool("cancel", false, "discard the staged preview")
		},
		Run: ctl.cmdTidy,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "rewind",
		Summary:       "Rewind the conversation to an earlier turn or mark",
		Args:          `[<handle> | --mark "<label>" | --apply | --undo | --cancel]`,
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 1},
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Flags: func(fs *flag.FlagSet) {
			fs.String("mark", "", "drop a named checkpoint at the current point")
			fs.Bool("apply", false, "confirm the staged rewind")
			fs.Bool("undo", false, "reverse the most recent rewind")
			fs.Bool("cancel", false, "discard the staged preview")
		},
		Examples: []string{"smith rewind", `smith rewind --mark "before refactor"`},
		Run:      ctl.cmdRewind,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "compact",
		Summary:       "Summarize older context into one reversible block",
		Args:          "[--apply | --undo | --cancel]",
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 0},
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("apply", false, "confirm the staged compaction")
			fs.Bool("undo", false, "restore the most recent compaction")
			fs.Bool("cancel", false, "discard the staged preview")
		},
		Examples: []string{"smith compact", "smith compact --apply"},
		Run:      ctl.cmdCompact,
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
		Name:          "goal",
		Summary:       "Set, show, or complete the session objective",
		Args:          `["<objective>" | done]`,
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{`smith goal "ship the parser"`, "smith goal", "smith goal done"},
		Run:           ctl.cmdGoal,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "budget",
		Summary:       "Set, show, or clear the session spend ceiling",
		Args:          "[<amount> | off]",
		Mode:          command.Inline,
		Scriptability: command.Both,
		Examples:      []string{"smith budget", "smith budget 0.50", "smith budget off"},
		Run:           ctl.cmdBudget,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "feature",
		Summary:       "Enter Coding Mode with a feature prompt",
		Args:          `"<prompt>"`,
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Examples:      []string{`smith feature "add OAuth login"`},
		Run:           ctl.cmdFeature,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "mode",
		Summary:       "Enter or exit Coding Mode, or show its status",
		Args:          "[coding | off]",
		Mode:          command.Inline,
		Scriptability: command.Both,
		Examples:      []string{"smith mode", "smith mode coding", "smith mode off"},
		Run:           ctl.cmdMode,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "phase",
		Summary:       "Advance, rewind, or jump to a Coding Mode phase",
		Args:          "[next | back | <name>]",
		Mode:          command.Inline,
		Scriptability: command.Both,
		Examples:      []string{"smith phase", "smith phase next", "smith phase verify"},
		Run:           ctl.cmdPhase,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "resume",
		Summary:       "List recent sessions, or load one by ID",
		Args:          "[id]",
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 1},
		Mode:          command.Inline,
		Scriptability: command.Both,
		Examples:      []string{"smith session list", "smith session resume <id>"},
		Run:           ctl.cmdResume,
	})
	mustRegisterCommand(reg, command.Command{
		Name:          "init",
		Summary:       "Scaffold project config and an AGENT.md memory file",
		Args:          "[--apply | --cancel]",
		ArgSpec:       &command.ArgSpec{Min: 0, Max: 0},
		Mode:          command.FullScreen,
		Scriptability: command.Both,
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("apply", false, "write the staged scaffold to disk")
			fs.Bool("cancel", false, "discard the staged scaffold")
		},
		Examples: []string{"smith init", "smith init --apply"},
		Run:      ctl.cmdInit,
	})
	mustRegisterCommand(reg, seriousCommand(ctl))
	return reg
}

// seriousCommand builds the /serious toggle (AS-053): it flips the Matrix
// personality layer's kill switch at runtime and reports the new state. Flavor
// is confined to interactive chrome, so the command is interactive-only — a
// headless/CI run already defaults to clean output and has no chrome to mute.
// When ctl (or its personality) is nil — the metadata-only enumeration used by
// the CLI router and the parity doc — it falls back to a throwaway personality
// so the descriptor still lists, exactly as the handler would behave.
func seriousCommand(ctl *chatSession) command.Command {
	// Resolve the personality at call time, not registration time, so the command
	// always toggles the live session's instance (set after this command is
	// registered) rather than a stale snapshot. ctl is nil only for the
	// metadata-only enumeration (CLI router / parity doc), where Run never fires.
	resolve := func() *personality.Personality {
		if ctl == nil {
			return personality.New(personality.Settings{}, true)
		}
		if ctl.pers == nil {
			ctl.pers = personality.New(personality.Settings{}, true)
		}
		return ctl.pers
	}
	return command.Command{
		Name:          "serious",
		Summary:       "Toggle the Matrix personality theme off/on for this session",
		Mode:          command.Inline,
		Scriptability: command.InteractiveOnly,
		Reason:        "mutes/restores interactive chrome flavor; non-interactive faces are already clean",
		Run: func(context.Context, []string) (command.Output, error) {
			if resolve().ToggleSerious() {
				return command.Output{Text: "Serious mode on — personality theme muted."}, nil
			}
			return command.Output{Text: "Serious mode off — personality theme restored."}, nil
		},
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

// builtinNames snapshots the names already in the registry — the built-ins — so a
// custom command (AS-033) is never allowed to shadow `/cost`, `/clear`, etc. The
// snapshot is taken once, before any custom command is layered on.
func builtinNames(reg *command.Registry) map[string]bool {
	names := map[string]bool{}
	for _, c := range reg.All() {
		names[c.Name] = true
	}
	return names
}

// registerCustomCommands discovers the user- and project-level custom commands
// (AS-033) and upserts each into reg as a command whose handler expands the
// template into a prompt for the model to run. A name colliding with a built-in
// is skipped so a dropped file can't hijack `/cost` and friends. It is called at
// startup and again whenever the palette opens, so a newly added file is picked
// up without a restart; a discovery error degrades to leaving the registry as-is
// rather than aborting the session.
func registerCustomCommands(reg *command.Registry, builtins map[string]bool, wd string) {
	cmds, err := customcmd.Load(customcmd.UserDir(), customcmd.ProjectDir(wd))
	if err != nil {
		return
	}
	for _, c := range cmds {
		if builtins[c.Name] {
			continue
		}
		cc := c // capture per iteration for the closure
		_ = reg.Upsert(command.Command{
			Name:    cc.Name,
			Summary: customSummary(cc),
			Args:    cc.ArgHint,
			Mode:    command.Inline,
			// The expansion is submitted as a model turn by the interactive face; the
			// headless face has no such submission seam yet, so it is marked
			// interactive-only with a Reason rather than silently no-op when scripted.
			Scriptability: command.InteractiveOnly,
			Reason:        "expands a prompt template into a model turn, which only the interactive face submits today",
			Run: func(_ context.Context, args []string) (command.Output, error) {
				return command.Output{Prompt: cc.Expand(args)}, nil
			},
		})
	}
}

// customSummary builds the /help one-liner for a custom command: its description
// (or a default) plus its source path, so it is visibly marked as custom and a
// project command that shadows a user one says so (AS-033).
func customSummary(c customcmd.Command) string {
	s := c.Description
	if s == "" {
		s = "Custom command"
	}
	tag := "custom: " + c.Source
	if c.Overrides {
		tag += "; overrides user command"
	}
	return s + " (" + tag + ")"
}

// shortID trims a session ID to a compact status-line label.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
