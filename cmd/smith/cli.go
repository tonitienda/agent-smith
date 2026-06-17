package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/openai"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
)

// commands builds the noun-grouped verb tree (D-CLI-1). The read-only verbs
// (`cost`, `context show`, `session list|resume`) dispatch through the same
// command.Registry the TUI palette renders (chatCommands), so a slash-command and
// its subcommand share one handler (D-CLI-10). The richer headless behaviour —
// streaming output, budgets, permission posture — lands on top in AS-051.
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
		configCommand(),
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
		Run:           registryCommand(regName, pickArgs),
	}
}

// bareTUI launches the interactive TUI for a bare `smith` on a terminal (D-CLI-2).
func bareTUI(*cli.Context) error { return startChat("", false) }

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
		Run: func(*cli.Context) error { return startChat(resume, noSplash) },
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
				Run:           registryCommand("resume", func(*cli.Context) []string { return nil }),
			},
			{
				Name:          "resume",
				Summary:       "Resume a session non-interactively",
				Usage:         "<id>",
				Examples:      []string{"smith session resume 1a2b3c4d"},
				Scriptability: resumable,
				Run:           registryCommand("resume", firstArg),
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
func registryCommand(name string, pickArgs func(*cli.Context) []string) func(*cli.Context) error {
	return func(c *cli.Context) error {
		ctl, closeFn, err := readonlyController()
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
func readonlyController() (*chatSession, func(), error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve working directory: %w", err)
	}
	store, err := session.NewStore("", wd)
	if err != nil {
		return nil, nil, fmt.Errorf("open session store: %w", err)
	}
	sess, err := latestSession(store)
	if err != nil {
		return nil, nil, err
	}
	pricing, err := cost.Default()
	if err != nil {
		return nil, nil, fmt.Errorf("load pricing table: %w", err)
	}
	providers := defaultProviders()
	provName, model := "anthropic", chatModel()
	if m := lastModel(sess.Log.Events()); m != "" {
		if r, ok := pricing.Lookup(m); ok && r.Vendor != "" {
			if _, ok := providers[r.Vendor]; ok {
				provName, model = r.Vendor, m
			}
		}
	}
	ctl := newChatSession(store, nil, pricing, providers, sess, provName, model, filepath.Base(wd))
	return ctl, func() { _ = sess.Log.Close() }, nil
}

// latestSession opens the project's most recently updated session for the
// read-only inspection verbs. When none exist yet it returns an ephemeral,
// in-memory session so `smith cost`/`context show`/`session list` render an empty
// state *without* creating a `.smith` session on disk — a read must never mutate
// the project.
func latestSession(store *session.Store) (*session.Session, error) {
	summaries, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if len(summaries) == 0 {
		return &session.Session{Log: eventlog.New()}, nil
	}
	sess, err := store.Open(summaries[0].ID) // List is newest-first
	if err != nil {
		return nil, fmt.Errorf("open session %q: %w", summaries[0].ID, err)
	}
	return sess, nil
}

// defaultProviders is the configured provider set (no network until a turn runs).
func defaultProviders() map[string]provider.Provider {
	return map[string]provider.Provider{
		"anthropic": anthropic.New(""),
		"openai":    openai.New(""),
	}
}

// runCommand is the one-task headless entry (D-CLI-3): the prompt arrives as a
// positional arg, via piped stdin (or `-`), or from `-f <file>`. It drives a
// single turn and writes the assistant's final text to stdout. Tool calls are
// denied by default — headless never prompts (D-CLI-8) and the allowlist/`--auto`
// posture (D-CLI-9) lands in AS-051 — so a turn that needs a tool stops with a
// structured reason rather than acting unattended.
func runCommand() *cli.Command {
	var file string
	return &cli.Command{
		Name:          "run",
		Summary:       "Run a single task non-interactively",
		Usage:         "<prompt>",
		Scriptability: command.Scriptable.String(),
		Examples: []string{
			`smith run "fix the failing test"`,
			`echo "summarize CHANGELOG" | smith run`,
			"smith run -f task.md",
		},
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&file, "f", "", "read the prompt from a file")
		},
		Run: func(c *cli.Context) error {
			prompt, err := resolvePrompt(c, file)
			if err != nil {
				return err
			}
			return runHeadless(context.Background(), c, prompt)
		},
	}
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

// runHeadless drives a single turn for `smith run`, writing the assistant's final
// text to stdout (D-CLI-5). It builds a fresh session and an engine with a
// deny-by-default tool gate.
func runHeadless(ctx context.Context, c *cli.Context, prompt string) error {
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

	tools, err := headlessTools(wd)
	if err != nil {
		return err
	}
	prov, model, err := headlessProvider()
	if err != nil {
		return err
	}
	rt := tool.NewRuntime(tools, sess.Log, tool.WithPermission(denyHeadless))
	eng, err := loop.New(prov, sess.Log, rt, tools, model)
	if err != nil {
		return fmt.Errorf("build engine: %w", err)
	}
	res, err := eng.Run(ctx, prompt)
	if err != nil {
		return err // runtime failure → exit 1
	}
	return c.Emit(res.FinalText)
}

// headlessProvider resolves the provider and model for a headless run from the
// configured default model (SMITH_MODEL, else defaultModel), mapping the model
// to its vendor through the pricing table — the same resolution readonlyController
// uses — so `smith run` with an OpenAI SMITH_MODEL talks to the OpenAI provider
// rather than failing against a hardcoded Anthropic one.
func headlessProvider() (provider.Provider, string, error) {
	pricing, err := cost.Default()
	if err != nil {
		return nil, "", fmt.Errorf("load pricing table: %w", err)
	}
	providers := defaultProviders()
	provName, model := "anthropic", chatModel()
	if r, ok := pricing.Lookup(model); ok && r.Vendor != "" {
		if _, ok := providers[r.Vendor]; ok {
			provName = r.Vendor
		}
	}
	prov, ok := providers[provName]
	if !ok {
		return nil, "", fmt.Errorf("no provider configured for model %q (vendor %q)", model, provName)
	}
	return prov, model, nil
}

// denyHeadless refuses every tool call: headless mode never opens an interactive
// permission prompt (D-CLI-8), and the allowlist/`--auto` posture is AS-051.
func denyHeadless(context.Context, tool.Call) tool.Decision {
	return tool.Denied("headless run denies tool calls by default; --auto/allowlist arrives in AS-051")
}

// headlessTools builds the built-in file and shell tools for a headless run.
func headlessTools(wd string) (*tool.Registry, error) {
	reg := tool.NewRegistry()
	fs, err := builtin.NewFS(wd)
	if err != nil {
		return nil, fmt.Errorf("init file tools: %w", err)
	}
	for _, t := range fs.Tools() {
		if err := reg.Register(t); err != nil {
			return nil, fmt.Errorf("register tool: %w", err)
		}
	}
	shell, err := builtin.NewShell(wd)
	if err != nil {
		return nil, fmt.Errorf("init shell tool: %w", err)
	}
	if err := reg.Register(shell); err != nil {
		return nil, fmt.Errorf("register shell tool: %w", err)
	}
	return reg, nil
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

// configGet resolves a key and prints its value to stdout; with --verbose it
// notes the source layer on stderr.
func configGet(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("config get: want exactly one key")
	}
	cfg, err := loadConfig(c.Globals.Config)
	if err != nil {
		return err
	}
	value, source, ok := cfg.Get(c.Args[0])
	if !ok {
		return fmt.Errorf("config: %q is not set", c.Args[0])
	}
	if c.Globals.Verbose {
		_, _ = fmt.Fprintf(c.Stderr, "(from %s)\n", source)
	}
	return c.Emit(value)
}

// knownConfigSections are the top-level config keys the binary recognizes.
// Anything else still loads and is preserved (forward-compat, PRD D2) but
// surfaces as a warning so a typo doesn't pass silently. The list grows as
// fast-follow features (subagents, personality, hooks, mcp) land their config.
var knownConfigSections = []string{
	"model", "provider", "permissions", "pricing",
	"subagents", "personality", "hooks", "mcp", "tools",
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
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

// loadLayeredConfig builds the nested-JSON config chain (AS-031) in D-CLI-6
// precedence, lowest to highest: built-in defaults, SMITH_* env, the user file,
// then the project file. Env sits *below* the files on purpose, so a checked-in
// repo config stays reproducible regardless of ambient environment (matching the
// flat chain in loadConfig). An explicit --config path replaces the user+project
// files with that single project-scoped file. The two chains consolidate in a
// follow-on (AS-071).
func loadLayeredConfig(override string) (*config.Config, error) {
	defaults := config.MapLayer("default", "built-in", map[string]any{"model": defaultModel})
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

// envConfigLayer maps recognized SMITH_* environment overrides into a config
// layer so `config show` reflects the same effective values the runtime sees.
// Only flat scalars are mapped today (e.g. SMITH_MODEL); richer env→nested-key
// mapping lands with the consumer migration (AS-071).
func envConfigLayer() config.Layer {
	values := map[string]any{}
	if v := strings.TrimSpace(os.Getenv(cli.EnvKey("model"))); v != "" {
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

// configSet writes a key=value to the explicit --config file when given,
// otherwise the project file (or the user file with --user), per D-CLI-6 /
// CLI-UX open-question 4 (project default). The confirmation line is a
// diagnostic, so --quiet suppresses it (D-CLI-5).
func configSet(c *cli.Context, user bool) error {
	if len(c.Args) != 2 {
		return cli.Usagef("config set: want <key> <value>")
	}
	path := c.Globals.Config
	if path == "" {
		p, err := configPath(user)
		if err != nil {
			return err
		}
		path = p
	}
	if err := cli.SaveConfigValue(path, c.Args[0], c.Args[1]); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if !c.Globals.Quiet {
		_, _ = fmt.Fprintf(c.Stderr, "set %s = %s in %s\n", c.Args[0], c.Args[1], path)
	}
	return nil
}

// loadConfig assembles the precedence chain (D-CLI-6). An explicit --config path
// (override != "") replaces the default project+user files with that single file
// — it "overrides the default chain" — while env and the built-in defaults still
// apply beneath it.
func loadConfig(override string) (cli.Config, error) {
	if override != "" {
		m, err := cli.LoadConfigFile(override)
		if err != nil {
			return cli.Config{}, fmt.Errorf("read config %q: %w", override, err)
		}
		return cli.Config{
			Project:  m,
			Getenv:   os.Getenv,
			Defaults: map[string]string{"model": defaultModel},
		}, nil
	}
	projectPath, err := configPath(false)
	if err != nil {
		return cli.Config{}, err
	}
	userPath, err := configPath(true)
	if err != nil {
		return cli.Config{}, err
	}
	project, err := cli.LoadConfigFile(projectPath)
	if err != nil {
		return cli.Config{}, fmt.Errorf("read project config: %w", err)
	}
	user, err := cli.LoadConfigFile(userPath)
	if err != nil {
		return cli.Config{}, fmt.Errorf("read user config: %w", err)
	}
	return cli.Config{
		Project:  project,
		User:     user,
		Getenv:   os.Getenv,
		Defaults: map[string]string{"model": defaultModel},
	}, nil
}

// configPath returns the project (./.smith/config) or user
// (<UserConfigDir>/smith/config) config file path.
func configPath(user bool) (string, error) {
	if user {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config dir: %w", err)
		}
		return filepath.Join(dir, "smith", "config"), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(wd, ".smith", "config"), nil
}
