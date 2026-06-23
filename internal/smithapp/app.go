// Package smithapp owns reusable Agent Smith application wiring that is shared
// by the executable faces. cmd/smith should stay a thin composition root: process
// flags, subcommand dispatch callbacks, and face-specific startup live there;
// provider/session/router construction that can be reused by tests or future
// faces lives here.
package smithapp

import (
	"fmt"
	"io"
	"os"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/credential"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/openai"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/internal/version"
)

// DefaultModel is the model a fresh Smith session starts on. SMITH_MODEL may
// override it through Runtime.ChatModel.
const DefaultModel = "claude-opus-4-8"

// CLIConfig is the process-level wiring needed to build the public smith router.
// Callers provide the command tree and bare handler because those still belong to
// the executable face; this package supplies the common app shell.
type CLIConfig struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	StdinTTY  bool
	StdoutTTY bool
	Getenv    func(string) string
	Bare      func(*cli.Context) error
	Commands  []*cli.Command
	Version   string
}

// BuildCLI builds the face-neutral smith router shell. It intentionally does not
// inspect os.Args or call os.Exit, keeping process entry in cmd/smith.
func BuildCLI(cfg CLIConfig) *cli.App {
	appVersion := cfg.Version
	if appVersion == "" {
		appVersion = version.String()
	}
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return &cli.App{
		Name:      "smith",
		Tagline:   "Agent Smith is a provider-agnostic coding agent harness.",
		Version:   appVersion,
		Stdin:     cfg.Stdin,
		Stdout:    cfg.Stdout,
		Stderr:    cfg.Stderr,
		StdinTTY:  cfg.StdinTTY,
		StdoutTTY: cfg.StdoutTTY,
		Getenv:    getenv,
		Bare:      cfg.Bare,
		Commands:  cfg.Commands,
	}
}

// RuntimeConfig contains injectable wiring for Smith runtime dependencies. A
// concrete Runtime keeps tests and future faces from mutating package-level
// globals while still letting the executable opt into the production defaults.
type RuntimeConfig struct {
	Providers func() map[string]provider.Provider
	Getenv    func(string) string
}

// Runtime owns reusable provider, model, session, and tool wiring for Smith
// faces. It is intentionally concrete rather than an App interface so callers can
// depend on only the methods they need.
type Runtime struct {
	providers func() map[string]provider.Provider
	getenv    func(string) string
}

// NewRuntime returns runtime wiring with production defaults filled in.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	providers := cfg.Providers
	if providers == nil {
		providers = DefaultProviders
	}
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Runtime{providers: providers, getenv: getenv}
}

// Providers resolves the configured provider set. Constructing the default set
// performs no network calls; providers connect only when a turn runs.
func (r *Runtime) Providers() map[string]provider.Provider { return r.providers() }

// ChatModel returns the configured starting model from SMITH_MODEL, or
// DefaultModel when unset.
func (r *Runtime) ChatModel() string {
	if v := r.getenv("SMITH_MODEL"); v != "" {
		return v
	}
	return DefaultModel
}

// SelectProviderModel maps a desired model to one of the available providers via
// the pricing table. When the model is unknown or its vendor is unavailable, it
// preserves the historical anthropic/default-model fallback.
func (r *Runtime) SelectProviderModel(pricing *cost.Table, providers map[string]provider.Provider, desired string) (string, string) {
	provName, model := "anthropic", desired
	if model == "" {
		model = r.ChatModel()
	}
	if r, ok := pricing.Lookup(model); ok && r.Vendor != "" {
		if _, ok := providers[r.Vendor]; ok {
			provName = r.Vendor
		}
	}
	return provName, model
}

// BuiltinTools builds the common built-in file and shell tools for Smith faces.
// fsOpts configure the file tools' shared FS — e.g. WithSnapshotter to capture
// pre-mutation file content for /rewind --restore-files (AS-084).
func (r *Runtime) BuiltinTools(wd string, fsOpts ...builtin.Option) (*tool.Registry, error) {
	reg := tool.NewRegistry()
	fs, err := builtin.NewFS(wd, fsOpts...)
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

// DefaultProviders is the production provider set. Each provider's API key
// resolves through the credential layer (AS-017): the provider env var wins,
// else the OS keychain. A missing key yields an empty string here and surfaces
// as a clear ErrAuth only when a turn actually runs — constructing the set stays
// network- and keychain-failure-free.
func DefaultProviders() map[string]provider.Provider {
	store := credential.Keyring{}
	anthKey, _ := credential.Resolve(os.Getenv, store, credential.Anthropic)
	openaiKey, _ := credential.Resolve(os.Getenv, store, credential.OpenAI)
	return map[string]provider.Provider{
		"anthropic": anthropic.New(anthKey),
		"openai":    openai.New(openaiKey),
	}
}

// OpenOrCreate resumes the session named by resumeID, or creates a fresh one
// when resumeID is empty.
func OpenOrCreate(store *session.Store, resumeID string) (*session.Session, error) {
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

// LatestSession opens the project's most recently updated session. When none
// exist yet it returns an ephemeral in-memory session so read-only commands do
// not mutate the project by creating a .smith session on disk.
func LatestSession(store *session.Store) (*session.Session, error) {
	summaries, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if len(summaries) == 0 {
		return &session.Session{Log: eventlog.New()}, nil
	}
	sess, err := store.Open(summaries[0].ID)
	if err != nil {
		return nil, fmt.Errorf("open session %q: %w", summaries[0].ID, err)
	}
	return sess, nil
}
