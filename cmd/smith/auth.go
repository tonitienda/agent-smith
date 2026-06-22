package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/credential"
)

// authStore is the secret backend the `smith auth` verbs manage. It is a package
// var so tests can substitute an in-memory fake and run without a host keychain
// (AS-017 AC: key lookup goes through a narrow internal interface).
var authStore credential.Store = credential.Keyring{}

// authCommand groups the credential verbs (AS-017, D9): keys live in the OS
// keychain — never a plaintext file — with the provider env vars as the override
// escape hatch. set/remove/status manage them per provider.
func authCommand() *cli.Command {
	return &cli.Command{
		Name:    "auth",
		Summary: "Manage provider API keys in the OS keychain",
		Sub: []*cli.Command{
			{
				Name:          "status",
				Summary:       "Report where each provider's key resolves from",
				Usage:         "[provider]",
				Examples:      []string{"smith auth status", "smith auth status anthropic"},
				Scriptability: command.Scriptable.String(),
				Run:           authStatus,
			},
			{
				Name:          "set",
				Summary:       "Store a provider's API key (read from stdin)",
				Usage:         "<provider>",
				Examples:      []string{"smith auth set anthropic", "echo $KEY | smith auth set openai"},
				Scriptability: command.Both.String(),
				Run:           authSet,
			},
			{
				Name:          "remove",
				Summary:       "Delete a provider's stored API key",
				Usage:         "<provider>",
				Examples:      []string{"smith auth remove openai"},
				Scriptability: command.Scriptable.String(),
				Run:           authRemove,
			},
		},
	}
}

// lookupProvider resolves the <provider> positional to a known credential
// account, returning a usage error that names the valid tokens otherwise.
func lookupProvider(name string) (credential.Provider, error) {
	if p, ok := credential.Lookup(name); ok {
		return p, nil
	}
	return credential.Provider{}, cli.Usagef("auth: unknown provider %q — want anthropic, openai, or openai-compatible:<name>", name)
}

// authStatus reports, without revealing any secret, whether each provider's key
// resolves from its env var, the keychain, or nowhere. With a provider argument
// it reports just that one.
func authStatus(c *cli.Context) error {
	var providers []credential.Provider
	switch len(c.Args) {
	case 0:
		providers = credential.Builtins()
	case 1:
		p, err := lookupProvider(c.Args[0])
		if err != nil {
			return err
		}
		providers = []credential.Provider{p}
	default:
		return cli.Usagef("auth status: want at most one provider")
	}

	var b strings.Builder
	for _, p := range providers {
		fmt.Fprintf(&b, "%s\t%s\n", p.Account, authSource(p))
	}
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

// authSource describes where p's key would resolve from, mirroring Resolve's
// precedence (env var over keychain) without exposing the value.
func authSource(p credential.Provider) string {
	if p.EnvVar != "" && strings.TrimSpace(os.Getenv(p.EnvVar)) != "" {
		return "set (env " + p.EnvVar + ")"
	}
	switch _, err := authStore.Get(p.Account); {
	case err == nil:
		return "set (keychain)"
	case errors.Is(err, credential.ErrUnavailable):
		return envHint("no keychain available", p)
	case errors.Is(err, credential.ErrNotFound):
		return envHint("not set", p)
	default:
		return fmt.Sprintf("error: %v", err)
	}
}

// envHint appends the env-var escape hatch to a "no stored key" status so the
// user always sees both ways to supply the key.
func envHint(state string, p credential.Provider) string {
	if p.EnvVar == "" {
		return state
	}
	return fmt.Sprintf("%s (set %s or `smith auth set %s`)", state, p.EnvVar, p.Account)
}

// authSet stores a provider key read from stdin: a no-echo prompt on a terminal,
// or the piped value otherwise. It never echoes or persists the key in plaintext;
// a missing keychain fails with an actionable message rather than falling back to
// a file.
func authSet(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("auth set: want <provider>")
	}
	p, err := lookupProvider(c.Args[0])
	if err != nil {
		return err
	}
	secret, err := readSecret(c, p)
	if err != nil {
		return err
	}
	if secret == "" {
		return cli.Usagef("auth set: empty key")
	}
	switch err := authStore.Set(p.Account, secret); {
	case errors.Is(err, credential.ErrUnavailable):
		return unavailableErr(p)
	case err != nil:
		return err
	}
	if !c.Globals.Quiet {
		_, _ = fmt.Fprintf(c.Stderr, "stored %s key in the OS keychain\n", p.Account)
	}
	return nil
}

// readSecret reads the key from a no-echo terminal prompt when stdin is a TTY,
// otherwise from piped stdin (trimmed).
func readSecret(c *cli.Context, p credential.Provider) (string, error) {
	if c.StdinTTY {
		_, _ = fmt.Fprintf(c.Stderr, "Enter %s API key (input hidden): ", p.Account)
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(c.Stderr)
		if err != nil {
			return "", fmt.Errorf("read key: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	// Cap the read: an API key is small, so a larger (or unbounded) pipe is
	// either a mistake or hostile — bound it rather than buffer it all.
	raw, err := io.ReadAll(io.LimitReader(c.Stdin, 4096))
	if err != nil {
		return "", fmt.Errorf("read key: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// authRemove deletes a provider's stored key.
func authRemove(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("auth remove: want <provider>")
	}
	p, err := lookupProvider(c.Args[0])
	if err != nil {
		return err
	}
	switch err := authStore.Remove(p.Account); {
	case errors.Is(err, credential.ErrNotFound):
		return fmt.Errorf("auth remove: no stored key for %q", p.Account)
	case errors.Is(err, credential.ErrUnavailable):
		return unavailableErr(p)
	case err != nil:
		return err
	}
	if !c.Globals.Quiet {
		_, _ = fmt.Fprintf(c.Stderr, "removed %s key from the OS keychain\n", p.Account)
	}
	return nil
}

// unavailableErr is the actionable error when no OS keychain is reachable: Smith
// will not write a plaintext key, so it points at the env-var escape hatch.
func unavailableErr(p credential.Provider) error {
	if p.EnvVar != "" {
		return fmt.Errorf("auth: no OS keychain available — Smith never writes a plaintext key; set %s for this process instead", p.EnvVar)
	}
	return errors.New("auth: no OS keychain available — Smith never writes a plaintext key")
}
