package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/hook"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/smithapp"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// commands builds the noun-grouped verb tree (D-CLI-1). The read-only verbs
// (`cost`, `context show`, `session list|resume`) dispatch through the same
// command.Registry the TUI palette renders (chatCommands), so a slash-command and
// its subcommand share one handler (D-CLI-10). The richer headless behaviour —
// streaming output, budgets, permission posture — lives on `run` (AS-051,
// headless.go).
func commands() []*cli.Command {
	// reg is a metadata-only view of the shared registry (no controller needed to
	// read names/summaries/scriptability); registryLeaf reads the descriptors so
	// shared verbs can't drift from their slash twins (AS-066).
	reg := chatCommands(nil)
	return []*cli.Command{
		runCommand(),
		sessionCommand(reg),
		contextCommand(reg),
		registryLeaf(reg, "cost", "cost", nil),
		registryLeaf(reg, "route", "route", nil),
		registryLeaf(reg, "insights", "insights", allArgs),
		configCommand(),
		serveCommand(),
		tuiCommand(),
	}
}

// registryLeaf builds a CLI leaf whose parity metadata — summary, usage (the arg
// spec), examples, scriptability, output schema — comes entirely from the shared
// command descriptor regName, so the slash command and its subcommand are one
// source of truth (AS-066). pickArgs maps the CLI positionals to the handler's
// args (nil passes none); the handler itself is the shared registry handler.
func registryLeaf(reg *command.Registry, regName, cliName string, pickArgs func(*cli.Context) []string) *cli.Command {
	desc, ok := reg.Lookup(regName)
	if !ok {
		panic(fmt.Sprintf("AS-066: CLI verb %q references unregistered command %q", cliName, regName))
	}
	return &cli.Command{
		Name:          cliName,
		Summary:       desc.Summary,
		Usage:         desc.Args,
		Examples:      desc.Examples,
		Scriptability: desc.Scriptability.String(),
		Reason:        desc.Reason,
		OutputSchema:  desc.OutputSchema,
		Run:           registryCommand(regName, desc.ArgSpec, pickArgs),
	}
}

// bareTUI launches the interactive TUI for a bare `smith` on a terminal (D-CLI-2).
func bareTUI(c *cli.Context) error { return startChat("", false, c.Globals.Config) }

// tuiCommand is the explicit TUI launch, with the prior --resume/--no-splash
// affordances preserved.
func tuiCommand() *cli.Command {
	var resume string
	var noSplash bool
	return &cli.Command{
		Name:          "tui",
		Summary:       "Launch the interactive TUI explicitly",
		Scriptability: command.InteractiveOnly.String(),
		Reason:        "launches the interactive terminal UI; use `smith run` to script a task",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&resume, "resume", "", "resume a session by ID")
			fs.BoolVar(&noSplash, "no-splash", false, "hide the startup header")
		},
		Run: func(c *cli.Context) error { return startChat(resume, noSplash, c.Globals.Config) },
	}
}

// sessionCommand groups the session verbs. The slash `/resume` fans out into two
// CLI verbs — `list` (no arg) and `resume <id>` — so they share the descriptor's
// scriptability (one source of truth, AS-066) while each carries its own usage and
// examples for the split. Both dispatch through the shared /resume handler, so the
// CLI and slash command stay one implementation. (Interactive picker is AS-064;
// `smith session resume <id>` is the canonical scriptable form, superseding the
// legacy `smith --resume <id>` flag.)
func sessionCommand(reg *command.Registry) *cli.Command {
	resumable := scriptability(reg, "resume")
	return &cli.Command{
		Name:    "session",
		Summary: "Inspect and resume sessions",
		Sub: []*cli.Command{
			{
				Name:          "list",
				Summary:       "List this project's sessions",
				Examples:      []string{"smith session list"},
				Scriptability: resumable,
				Run:           registryCommand("resume", &command.ArgSpec{Min: 0, Max: 0}, func(*cli.Context) []string { return nil }),
			},
			{
				Name:          "resume",
				Summary:       "Resume a session non-interactively",
				Usage:         "<id>",
				Examples:      []string{"smith session resume 1a2b3c4d"},
				Scriptability: resumable,
				Run:           registryCommand("resume", &command.ArgSpec{Min: 1, Max: 1}, firstArg),
			},
		},
	}
}

// contextCommand groups the context verbs. `show` is a 1:1 mapping of /context, so
// it sources all of its parity metadata from the shared descriptor (AS-066).
func contextCommand(reg *command.Registry) *cli.Command {
	return &cli.Command{
		Name:    "context",
		Summary: "Inspect the model's context window",
		Sub:     []*cli.Command{registryLeaf(reg, "context", "show", allArgs)},
	}
}

// scriptability returns the registered command's scriptability string, panicking
// if it isn't registered — the verb tree is static, so a miss is a wiring bug.
func scriptability(reg *command.Registry, name string) string {
	desc, ok := reg.Lookup(name)
	if !ok {
		panic(fmt.Sprintf("AS-066: command %q is not registered", name))
	}
	return desc.Scriptability.String()
}

// firstArg passes only the first positional through to the shared handler.
func firstArg(c *cli.Context) []string {
	if len(c.Args) == 0 {
		return nil
	}
	return c.Args[:1]
}

// allArgs passes every positional through.
func allArgs(c *cli.Context) []string { return c.Args }

// registryCommand dispatches a CLI verb to the shared command.Registry handler
// named name, so the subcommand and its slash command run identical code
// (D-CLI-10). pickArgs maps the CLI positionals to the handler's args (nil passes
// none). The handler runs over a read-only controller bound to the project's most
// recent session.
func registryCommand(name string, argSpec *command.ArgSpec, pickArgs func(*cli.Context) []string) func(*cli.Context) error {
	return func(c *cli.Context) error {
		// Arity is static descriptor metadata, so enforce it over the raw
		// positionals before opening a session: the subcommand rejects the same
		// counts the slash command does — even where pickArgs would drop extras —
		// at no cost when the usage is wrong (AS-090).
		probe := command.Command{Name: name, ArgSpec: argSpec}
		if err := probe.CheckArity(c.Args); err != nil {
			return cli.Usagef("%s: %v", name, err)
		}
		ctl, closeFn, err := readonlyController(c.Globals.Config)
		if err != nil {
			return err
		}
		defer closeFn()

		cmd, ok := chatCommands(ctl).Lookup(name)
		if !ok {
			return fmt.Errorf("command %q is not registered", name)
		}
		var args []string
		if pickArgs != nil {
			args = pickArgs(c)
		}
		out, err := cmd.Run(context.Background(), args)
		if err != nil {
			return err
		}
		return c.Emit(out.Text)
	}
}

// readonlyController builds a controller over the project's most recent session
// for the read-only inspection verbs (cost, context, session). It wires the
// pricing table and providers but never builds an engine, so it issues no model
// calls. closeFn releases the opened session log.
func readonlyController(override string) (*chatSession, func(), error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve working directory: %w", err)
	}
	store, err := session.NewStore("", wd)
	if err != nil {
		return nil, nil, fmt.Errorf("open session store: %w", err)
	}
	sess, err := smithapp.LatestSession(store)
	if err != nil {
		return nil, nil, err
	}
	pricing, err := sessionPricing(override)
	if err != nil {
		return nil, nil, fmt.Errorf("load pricing table: %w", err)
	}
	providers := appRuntime.Providers()
	model := appRuntime.ChatModel()
	if m := lastModel(sess.Log.Events()); m != "" {
		model = m
	}
	provName, model := appRuntime.SelectProviderModel(pricing, providers, model)
	ctl := newChatSession(store, nil, pricing, providers, sess, provName, model, wd, nil, nil)
	// Apply the configured routing policy (AS-042) so `smith route` shows the same
	// tiers the interactive face does. A config load error degrades to the default
	// policy rather than failing a read-only inspection.
	if cfg, err := loadLayeredConfig(override); err == nil {
		routingCfg, _ := routing.ConfigFrom(cfg)
		ctl.setRouter(routingCfg)
	}
	return ctl, func() { _ = sess.Log.Close() }, nil
}

// runCommand is the one-task headless entry (D-CLI-3): the prompt arrives as a
// positional arg, via piped stdin (or `-`), or from `-f <file>`. It drives a
// single turn and writes the assistant's final text to stdout. Tool calls are
// denied by default — headless never prompts (D-CLI-8) and the allowlist/`--auto`
// posture (D-CLI-9) lands in AS-051 — so a turn that needs a tool stops with a
// structured reason rather than acting unattended.
func runCommand() *cli.Command {
	var file string
	var budgetFlag string
	var auto bool
	return &cli.Command{
		Name:          "run",
		Summary:       "Run a single task non-interactively",
		Usage:         "<prompt>",
		Scriptability: command.Scriptable.String(),
		OutputSchema:  "text, session_id, stop_reason, cost_usd, iterations, denied[]",
		Examples: []string{
			`smith run "fix the failing test"`,
			`echo "summarize CHANGELOG" | smith run`,
			"smith run -f task.md",
			`smith run "ship it" --output json --budget 0.25 --auto`,
		},
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&file, "f", "", "read the prompt from a file")
			fs.StringVar(&budgetFlag, "budget", "", "halt the run at this dollar ceiling (e.g. 0.25)")
			fs.BoolVar(&auto, "auto", false, "auto-approve tool calls (unattended); default denies what would prompt")
		},
		Run: func(c *cli.Context) error {
			prompt, err := resolvePrompt(c, file)
			if err != nil {
				return err
			}
			budgetUSD, err := parseBudgetFlag(budgetFlag)
			if err != nil {
				return err
			}
			// Cancel the run gracefully on Ctrl+C so the loop reconciles any in-flight
			// tool call rather than the OS killing the process mid-turn.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			return runHeadless(ctx, c, prompt, headlessOpts{budgetUSD: budgetUSD, auto: auto})
		},
	}
}

// parseBudgetFlag parses the --budget value into a dollar ceiling, tolerating a
// leading currency symbol the way /budget does. An empty value means "no
// ceiling" (0). A negative or unparseable amount is a usage error.
func parseBudgetFlag(v string) (float64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	amount, err := strconv.ParseFloat(strings.TrimPrefix(v, "$"), 64)
	if err != nil || amount < 0 {
		return 0, cli.Usagef("run: invalid --budget %q: want a non-negative dollar amount", v)
	}
	return amount, nil
}

// resolvePrompt resolves the task prompt per D-CLI-3 precedence: a positional
// argument, then stdin (piped, or an explicit `-`), then `-f <file>`. It is a
// usage error to supply none.
//
// A non-TTY signals only that stdin *might* be piped, not that data was actually
// sent (CI, a script, or stdin redirected from /dev/null are all non-TTY with an
// empty stream). So when nothing is piped we fall back to -f rather than erroring,
// which keeps -f usable in its primary scripted environment (AS-069) while genuinely
// piped data still outranks the file per D-CLI-3 #2.
func resolvePrompt(c *cli.Context, file string) (string, error) {
	switch {
	case len(c.Args) == 1 && c.Args[0] == "-":
		s, err := readTrim(c.Stdin)
		if err != nil {
			return "", err
		}
		return nonEmptyPrompt(s)
	case len(c.Args) > 0:
		return strings.Join(c.Args, " "), nil
	case !c.StdinTTY:
		raw, err := readRaw(c.Stdin)
		if err != nil {
			return "", err
		}
		// Bytes on stdin mean data was actually piped, so stdin is the source and
		// outranks -f per D-CLI-3 #2 — a blank pipe (`printf "\n" | smith run`)
		// surfaces as "empty prompt", not a silent fall-through to the file.
		if len(raw) > 0 {
			return nonEmptyPrompt(strings.TrimSpace(string(raw)))
		}
		// Zero bytes means nothing was piped (CI, a script, stdin from /dev/null):
		// fall back to -f, else report no prompt at all (AS-069).
		if file != "" {
			return readFile(file)
		}
		return "", errNoPrompt()
	case file != "":
		return readFile(file)
	default:
		return "", errNoPrompt()
	}
}

// errNoPrompt is the usage error for `run` invoked with no prompt source.
func errNoPrompt() error {
	return cli.Usagef("run: no prompt — pass it as an argument, pipe it on stdin, or use -f <file>")
}

// readRaw reads r fully without trimming, so callers can tell a truly empty
// stream (0 bytes) from blank-but-piped input. A nil reader yields no bytes
// rather than panicking, so a face that leaves Stdin unset is safe.
func readRaw(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read prompt: %w", err)
	}
	return b, nil
}

// readTrim reads r fully and trims surrounding whitespace; an empty result is
// allowed (callers decide whether emptiness is an error).
func readTrim(r io.Reader) (string, error) {
	b, err := readRaw(r)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// readFile reads the -f prompt file and trims surrounding whitespace, rejecting
// an empty result so `smith run -f <emptyfile>` gives the same usage error as an
// empty stdin rather than running with no prompt.
func readFile(file string) (string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read prompt file: %w", err)
	}
	return nonEmptyPrompt(strings.TrimSpace(string(b)))
}

// nonEmptyPrompt returns s, or a usage error when s is empty.
func nonEmptyPrompt(s string) (string, error) {
	if s == "" {
		return "", cli.Usagef("run: empty prompt")
	}
	return s, nil
}

// headlessOpts carries the run-specific posture flags `smith run` resolves from
// its flags: a dollar budget ceiling (0 = unmetered) and whether to auto-approve
// tool calls (AS-051).
type headlessOpts struct {
	budgetUSD float64
	auto      bool
}

// runHeadless drives a single task for `smith run` and renders the outcome per
// --output (D-CLI-4): the assistant's final text on stdout for plain mode, or a
// structured runResult (answer, cost, session id, stop reason, permission denials)
// for json/stream-json. It builds a fresh session — resumable later (AS-051 AC4) —
// wires the allowlist-then-deny permission posture (D-CLI-9, or auto with
// `--auto`), the lifecycle hooks (AS-035), and budget enforcement (AS-041/AS-086)
// when --budget is set, then maps how the run ended to the additive exit-code
// taxonomy (permission/budget/cancellation/provider).
func runHeadless(ctx context.Context, c *cli.Context, prompt string, opts headlessOpts) error {
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
	defer func() { _ = sess.Log.Close() }()
	// A headless run is a fresh session, so seed the project's memory files
	// (AS-032) before the single turn — the model follows the same standing
	// guidance an interactive session does.
	if err := seedMemory(wd, sess); err != nil {
		return err
	}

	tools, err := appRuntime.BuiltinTools(wd)
	if err != nil {
		return err
	}
	prov, model, err := headlessProvider(c.Globals.Config)
	if err != nil {
		return err
	}
	pricing, err := sessionPricing(c.Globals.Config)
	if err != nil {
		return fmt.Errorf("load pricing table: %w", err)
	}

	// Permission posture (D-CLI-9): allowlist-then-deny by default, auto with
	// --auto. The gate is wrapped so denied calls surface in the result.
	gate, err := headlessPermission(wd, c.Globals.Config, opts.auto)
	if err != nil {
		return fmt.Errorf("load permission policy: %w", err)
	}

	// Load and wire the lifecycle hooks (AS-035) so a headless run honors the same
	// pre/post-tool-use and prompt-submit automation an interactive session does.
	cfg, err := loadLayeredConfig(c.Globals.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hooks := loadHooks(cfg, c.Stderr)
	// Capture-time redaction (AS-115): a headless run honors the same `redaction`
	// opt-in an interactive session does, scrubbing secrets before the log.
	applyRedaction(cfg, sess.Log, c.Stderr)

	rtOpts := append([]tool.Option{tool.WithPermission(gate.decide)}, hookToolOptions(hooks, sess.Log, sess.ID)...)
	rt := tool.NewRuntime(tools, sess.Log, rtOpts...)

	engOpts := []loop.Option{}
	if obs := streamObserver(c); obs != nil {
		engOpts = append(engOpts, loop.WithObserver(obs))
	}
	// Budget enforcement (AS-041/AS-086): --budget sets the ceiling for this run,
	// priced against the same table as /cost. Without --budget the run is unmetered.
	if opts.budgetUSD > 0 {
		log := sess.Log
		spent := func() float64 { return cost.Summarize(log.Events(), pricing).TotalUSD }
		reserve := func(c []schema.Block) (float64, bool) { return cost.EstimateTurnCostUSD(c, model, pricing) }
		engOpts = append(engOpts,
			loop.WithBudget(spent, opts.budgetUSD, 0),
			loop.WithBudgetReservation(reserve, false),
		)
	}
	// Sub-agent lifecycle (AS-107): a headless run drives the same built-in passive
	// analyzers (AS-048) an interactive session does, with the `subagents.<name>`
	// config overlay. One Runner over the run's single session; the loop tears it
	// down off the streaming path, so a one-shot run still surfaces findings.
	// A headless run does not load the skill tool, so no fact can be skill-scoped;
	// the resolver only needs the working-directory memory tree. The durable ledger
	// is shared with interactive sessions of the same project (AS-108).
	subReg, subStore, err := buildSubAgents(cfg, saveTargetResolver(wd, nil), openFactLedger(store, c.Stderr), nil, c.Stderr)
	if err != nil {
		return fmt.Errorf("build sub-agents: %w", err)
	}
	engOpts = append(engOpts, loop.WithSubAgents(subagent.NewRunner(subReg, subStore, sess.ID)))
	eng, err := loop.New(prov, sess.Log, rt, tools, model, engOpts...)
	if err != nil {
		return fmt.Errorf("build engine: %w", err)
	}

	fireLifecycle(ctx, hooks, sess.Log, hook.Payload{Event: hook.SessionStart, Session: sess.ID})
	defer fireLifecycle(context.Background(), hooks, sess.Log, hook.Payload{Event: hook.SessionStop, Session: sess.ID})

	if out := fireLifecycle(ctx, hooks, sess.Log, hook.Payload{Event: hook.UserPromptSubmit, Session: sess.ID, Prompt: prompt}); out.Blocked {
		return fmt.Errorf("prompt blocked by hook: %s", out.Reason)
	} else if rewritten := promptRewrite(out.Input); rewritten != "" {
		prompt = rewritten
	}

	res, runErr := eng.Run(ctx, prompt)
	totalUSD := cost.Summarize(sess.Log.Events(), pricing).TotalUSD
	return emitResult(c, sess.ID, res, totalUSD, gate.denied(), runErr)
}

// headlessProvider resolves the provider and model for a headless run from the
// configured default model (SMITH_MODEL, else smithapp.DefaultModel), mapping the model
// to its vendor through the pricing table — the same resolution readonlyController
// uses — so `smith run` with an OpenAI SMITH_MODEL talks to the OpenAI provider
// rather than failing against a hardcoded Anthropic one.
func headlessProvider(override string) (provider.Provider, string, error) {
	pricing, err := sessionPricing(override)
	if err != nil {
		return nil, "", fmt.Errorf("load pricing table: %w", err)
	}
	providers := appRuntime.Providers()
	provName, model := appRuntime.SelectProviderModel(pricing, providers, appRuntime.ChatModel())
	prov, ok := providers[provName]
	if !ok {
		return nil, "", fmt.Errorf("no provider configured for model %q (vendor %q)", model, provName)
	}
	return prov, model, nil
}

// configCommand reads and writes layered config (D-CLI-6). `get` resolves a key
// through the precedence chain; `set` writes to the project file by default
// (`--user` targets the user file). This is the CLI's slice of the full config
// system (AS-031).
func configCommand() *cli.Command {
	var user bool
	return &cli.Command{
		Name:    "config",
		Summary: "Read and write layered configuration",
		Sub: []*cli.Command{
			{
				Name:          "show",
				Summary:       "Print the effective merged config and each value's source layer",
				Examples:      []string{"smith config show"},
				Scriptability: command.Scriptable.String(),
				Run:           configShow,
			},
			{
				Name:          "get",
				Summary:       "Resolve a config key through the precedence chain",
				Usage:         "<key>",
				Examples:      []string{"smith config get model"},
				Scriptability: command.Scriptable.String(),
				Run:           configGet,
			},
			{
				Name:          "set",
				Summary:       "Set a config key (project scope by default)",
				Usage:         "<key> <value>",
				Examples:      []string{"smith config set model claude-opus-4-8", "smith config set model gpt-4o --user"},
				Scriptability: command.Scriptable.String(),
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&user, "user", false, "write to user config instead of the project")
				},
				Run: func(c *cli.Context) error { return configSet(c, user) },
			},
		},
	}
}

// configGet resolves a dotted key through the layered config and prints its
// value to stdout; with --verbose it notes the source layer on stderr. A leaf
// prints as a scalar; an interior section prints as JSON.
func configGet(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("config get: want exactly one key")
	}
	cfg, err := loadLayeredConfig(c.Globals.Config)
	if err != nil {
		return err
	}
	value, source, ok := cfg.Value(c.Args[0])
	if !ok {
		return fmt.Errorf("config: %q is not set", c.Args[0])
	}
	if c.Globals.Verbose && source.Layer != "" {
		_, _ = fmt.Fprintf(c.Stderr, "(from %s)\n", source)
	}
	return c.Emit(formatConfigValue(value))
}

// formatConfigValue renders a resolved config value for `config get`: a string
// prints as-is, anything else (numbers, bools, objects from a nested section) as
// JSON so the output round-trips.
func formatConfigValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// knownConfigSections are the top-level config keys the binary recognizes.
// Anything else still loads and is preserved (forward-compat, PRD D2) but
// surfaces as a warning so a typo doesn't pass silently. The list grows as
// fast-follow features (subagents, personality, hooks, mcp) land their config.
var knownConfigSections = []string{
	"model", "provider", "permissions", "pricing",
	"subagents", "personality", "hooks", "mcp", "tools",
	"budget", "compact",
}

// configShow loads the layered JSON config (built-in defaults < user < project)
// and prints every effective value with the layer that won it. Unknown
// top-level keys are reported on stderr without being dropped (AS-031).
func configShow(c *cli.Context) error {
	cfg, err := loadLayeredConfig(c.Globals.Config)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range cfg.Effective() {
		fmt.Fprintf(&b, "%s = %v\t[%s]\n", e.Path, e.Value, e.Source)
	}
	for _, w := range cfg.Unknown(knownConfigSections...) {
		_, _ = fmt.Fprintf(c.Stderr, "warning: %s\n", w)
	}
	// The flat file is never read now, regardless of --config, so warn whenever
	// one is present (matching ADR-0002) rather than only on the default chain.
	warnLegacyFlatConfig(c.Stderr)
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

// warnLegacyFlatConfig warns when a pre-AS-071 flat `key = value` config file
// (the project ./.smith/config or the user <UserConfigDir>/smith/config) is
// still present. AS-071 replaced the flat chain with nested `config.json`; the
// flat file is no longer read, so without this warning its settings would be
// silently ignored (PRD D0: punt explicitly, never silently). The migration is
// to move the keys into the sibling `config.json`.
func warnLegacyFlatConfig(stderr io.Writer) {
	wd, err := os.Getwd()
	if err == nil {
		legacyFlatWarn(stderr, filepath.Join(wd, ".smith", "config"))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		legacyFlatWarn(stderr, filepath.Join(dir, "smith", "config"))
	}
}

func legacyFlatWarn(stderr io.Writer, path string) {
	if _, err := os.Stat(path); err == nil {
		_, _ = fmt.Fprintf(stderr, "warning: legacy flat config %s is no longer read; move its keys into %s.json (AS-071)\n", path, path)
	}
}

// loadLayeredConfig builds the nested-JSON config chain (AS-031) in D-CLI-6
// precedence, lowest to highest: built-in defaults, SMITH_* env, the user file,
// then the project file. Env sits *below* the files on purpose, so a checked-in
// repo config stays reproducible regardless of ambient environment. An explicit
// --config path replaces the user+project files with that single project-scoped
// file. This is the single config loader (AS-071): `config get`/`set`/`show`,
// pricing, and permissions all resolve through it.
func loadLayeredConfig(override string) (*config.Config, error) {
	defaults := config.MapLayer("default", "built-in", map[string]any{"model": smithapp.DefaultModel})
	env := envConfigLayer()
	if override != "" {
		project, err := config.FileLayer("project", override)
		if err != nil {
			return nil, err
		}
		return config.New(defaults, env, project), nil
	}
	userPath, err := layeredConfigPath(true)
	if err != nil {
		return nil, err
	}
	projectPath, err := layeredConfigPath(false)
	if err != nil {
		return nil, err
	}
	user, err := config.FileLayer("user", userPath)
	if err != nil {
		return nil, err
	}
	project, err := config.FileLayer("project", projectPath)
	if err != nil {
		return nil, err
	}
	return config.New(defaults, env, user, project), nil
}

// sessionPricing builds the pricing table from the embedded defaults overlaid
// with the unified config's `pricing` section and the $SMITH_PRICING escape
// hatch (AS-071). override is the --config path (empty for the default chain),
// so `smith run --config x.json` prices through x.json's pricing section.
func sessionPricing(override string) (*cost.Table, error) {
	cfg, err := loadLayeredConfig(override)
	if err != nil {
		return nil, err
	}
	var section []byte
	if v, _, ok := cfg.Value("pricing"); ok {
		if section, err = json.Marshal(v); err != nil {
			return nil, fmt.Errorf("encode pricing config: %w", err)
		}
	}
	return cost.DefaultWith(section)
}

// envConfigLayer maps recognized SMITH_* environment overrides into a config
// layer so `config show` reflects the same effective values the runtime sees.
// Only flat scalars are mapped today (e.g. SMITH_MODEL); richer env→nested-key
// mapping can grow here as sections gain env overrides.
func envConfigLayer() config.Layer {
	values := map[string]any{}
	if v := strings.TrimSpace(os.Getenv("SMITH_MODEL")); v != "" {
		values["model"] = v
	}
	return config.MapLayer("env", "env", values)
}

// layeredConfigPath returns the project (./.smith/config.json) or user
// (<UserConfigDir>/smith/config.json) nested-config path — the `.json` sibling
// of the flat `config` file used by `config get`/`set`.
func layeredConfigPath(user bool) (string, error) {
	if user {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config dir: %w", err)
		}
		return filepath.Join(dir, "smith", "config.json"), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(wd, ".smith", "config.json"), nil
}

// configSet writes a dotted key to the explicit --config file when given,
// otherwise the project file (or the user file with --user), per D-CLI-6 /
// CLI-UX open-question 4 (project default). The value is stored as a string in
// the nested JSON config; typed values (pricing numbers, lists) are edited in
// the file directly. The confirmation line is a diagnostic, so --quiet
// suppresses it (D-CLI-5).
func configSet(c *cli.Context, user bool) error {
	if len(c.Args) != 2 {
		return cli.Usagef("config set: want <key> <value>")
	}
	path := c.Globals.Config
	if path == "" {
		p, err := layeredConfigPath(user)
		if err != nil {
			return err
		}
		path = p
	}
	if err := config.SetFileValue(path, c.Args[0], c.Args[1]); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if !c.Globals.Quiet {
		_, _ = fmt.Fprintf(c.Stderr, "set %s = %s in %s\n", c.Args[0], c.Args[1], path)
	}
	return nil
}
