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
	"github.com/tonitienda/agent-smith/internal/cost"
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
	return []*cli.Command{
		runCommand(),
		sessionCommand(),
		contextCommand(),
		{
			Name:     "cost",
			Summary:  "Show token & cost accounting for the session",
			Examples: []string{"smith cost", "smith cost --output json"},
			Run:      registryCommand("cost", nil),
		},
		configCommand(),
		tuiCommand(),
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
		Name:    "tui",
		Summary: "Launch the interactive TUI explicitly",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&resume, "resume", "", "resume a session by ID")
			fs.BoolVar(&noSplash, "no-splash", false, "hide the startup header")
		},
		Run: func(*cli.Context) error { return startChat(resume, noSplash) },
	}
}

// sessionCommand groups the session verbs. Both dispatch through the registry's
// /resume handler (no args lists, an ID loads), so the CLI and slash command stay
// one implementation. (The interactive resume picker is AS-064.)
func sessionCommand() *cli.Command {
	return &cli.Command{
		Name:    "session",
		Summary: "Inspect and resume sessions",
		Sub: []*cli.Command{
			{
				Name:     "list",
				Summary:  "List this project's sessions",
				Examples: []string{"smith session list"},
				Run:      registryCommand("resume", func(*cli.Context) []string { return nil }),
			},
			{
				Name:     "resume",
				Summary:  "Resume a session non-interactively",
				Usage:    "<id>",
				Examples: []string{"smith session resume 1a2b3c4d"},
				Run:      registryCommand("resume", firstArg),
			},
		},
	}
}

// contextCommand groups the context verbs. `show` reuses the /context handler.
func contextCommand() *cli.Command {
	return &cli.Command{
		Name:    "context",
		Summary: "Inspect the model's context window",
		Sub: []*cli.Command{
			{
				Name:     "show",
				Summary:  "Show what is filling the window",
				Usage:    "[size|age|type]",
				Examples: []string{"smith context show", "smith context show age"},
				Run:      registryCommand("context", allArgs),
			},
		},
	}
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

// latestSession opens the project's most recently updated session, or creates a
// fresh empty one when none exists yet (so `smith cost` on a clean project shows
// an empty session rather than erroring).
func latestSession(store *session.Store) (*session.Session, error) {
	summaries, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if len(summaries) == 0 {
		sess, err := store.Create("")
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		return sess, nil
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
		Name:    "run",
		Summary: "Run a single task non-interactively",
		Usage:   "<prompt>",
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
func resolvePrompt(c *cli.Context, file string) (string, error) {
	switch {
	case len(c.Args) == 1 && c.Args[0] == "-":
		return readAllTrim(c.Stdin)
	case len(c.Args) > 0:
		return strings.Join(c.Args, " "), nil
	case !c.StdinTTY:
		return readAllTrim(c.Stdin)
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	default:
		return "", cli.Usagef("run: no prompt — pass it as an argument, pipe it on stdin, or use -f <file>")
	}
}

// readAllTrim reads r fully and trims surrounding whitespace.
func readAllTrim(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read prompt: %w", err)
	}
	s := strings.TrimSpace(string(b))
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
				Name:     "get",
				Summary:  "Resolve a config key through the precedence chain",
				Usage:    "<key>",
				Examples: []string{"smith config get model"},
				Run:      configGet,
			},
			{
				Name:     "set",
				Summary:  "Set a config key (project scope by default)",
				Usage:    "<key> <value>",
				Examples: []string{"smith config set model claude-opus-4-8", "smith config set model gpt-4o --user"},
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
	cfg, err := loadConfig()
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

// configSet writes a key=value to the project file (or the user file with
// --user), per D-CLI-6 / CLI-UX open-question 4 (project default).
func configSet(c *cli.Context, user bool) error {
	if len(c.Args) != 2 {
		return cli.Usagef("config set: want <key> <value>")
	}
	path, err := configPath(user)
	if err != nil {
		return err
	}
	if err := cli.SaveConfigValue(path, c.Args[0], c.Args[1]); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	_, _ = fmt.Fprintf(c.Stderr, "set %s = %s in %s\n", c.Args[0], c.Args[1], path)
	return nil
}

// loadConfig assembles the precedence chain from the project and user files plus
// SMITH_* env and the built-in defaults (D-CLI-6).
func loadConfig() (cli.Config, error) {
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
