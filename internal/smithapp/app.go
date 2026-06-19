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
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/openai"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/version"
)

// DefaultModel is the model a fresh Smith session starts on. SMITH_MODEL may
// override it through ChatModel.
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
	version := cfg.Version
	if version == "" {
		version = versionString()
	}
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return &cli.App{
		Name:      "smith",
		Tagline:   "Agent Smith is a provider-agnostic coding agent harness.",
		Version:   version,
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

func versionString() string { return version.String() }

// ProvidersFn resolves the configured provider set. It is a package variable so
// tests can substitute mocks without network-backed providers.
var ProvidersFn = DefaultProviders

// DefaultProviders is the configured provider set. Constructing it performs no
// network calls; providers connect only when a turn runs.
func DefaultProviders() map[string]provider.Provider {
	return map[string]provider.Provider{
		"anthropic": anthropic.New(""),
		"openai":    openai.New(""),
	}
}

// ChatModel returns the configured starting model from SMITH_MODEL, or
// DefaultModel when unset.
func ChatModel() string {
	if v := os.Getenv("SMITH_MODEL"); v != "" {
		return v
	}
	return DefaultModel
}

// SelectProviderModel maps a desired model to one of the available providers via
// the pricing table. When the model is unknown or its vendor is unavailable, it
// preserves the historical anthropic/default-model fallback.
func SelectProviderModel(pricing *cost.Table, providers map[string]provider.Provider, desired string) (string, string) {
	provName, model := "anthropic", desired
	if model == "" {
		model = ChatModel()
	}
	if r, ok := pricing.Lookup(model); ok && r.Vendor != "" {
		if _, ok := providers[r.Vendor]; ok {
			provName = r.Vendor
		}
	}
	return provName, model
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
