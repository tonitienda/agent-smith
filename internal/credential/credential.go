// Package credential resolves provider API keys from the OS keychain with an
// environment-variable escape hatch (AS-017, PRD D9). Agent Smith never writes a
// key to disk in plaintext: the only persistent store is the operating system's
// secret service, reached through the narrow Store seam so tests run without a
// host keychain. Environment variables always override a stored key, which is how
// CI and headless runs supply credentials.
package credential

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// Service is the keychain service namespace for every Smith credential.
const Service = "agent-smith"

// compatPrefix marks an OpenAI-compatible endpoint's account name
// (`openai-compatible:<config-name>`), per the AS-017 namespacing decision.
const compatPrefix = "openai-compatible:"

var (
	// ErrNotFound reports that no credential is stored for an account.
	ErrNotFound = errors.New("credential not found")
	// ErrUnavailable reports that no OS secret store is reachable, so Smith cannot
	// store a key without writing plaintext — which it refuses to do.
	ErrUnavailable = errors.New("no OS keychain available")
)

// Store is the narrow secret-storage seam. The production implementation is the
// OS keychain (Keyring); tests substitute an in-memory fake so they never touch
// the host secret service.
type Store interface {
	// Get returns the stored secret for account, or ErrNotFound when unset.
	Get(account string) (string, error)
	// Set stores secret under account, replacing any existing value.
	Set(account, secret string) error
	// Remove deletes account's secret, returning ErrNotFound when unset.
	Remove(account string) error
}

// Provider identifies a credential by its stable keychain account name (also the
// `smith auth` <provider> token) and the environment variable that overrides it.
// An empty EnvVar means the account has no env escape hatch (keychain only).
type Provider struct {
	Account string
	EnvVar  string
}

// The built-in providers. OpenAI-compatible endpoints use Compat to derive a
// per-endpoint account with no env escape hatch.
var (
	Anthropic = Provider{Account: "anthropic", EnvVar: "ANTHROPIC_API_KEY"}
	OpenAI    = Provider{Account: "openai", EnvVar: "OPENAI_API_KEY"}
)

// Builtins are the providers `smith auth status` reports by default.
func Builtins() []Provider { return []Provider{Anthropic, OpenAI} }

// Compat returns the keychain-only Provider for an OpenAI-compatible endpoint
// named by its config-name.
func Compat(configName string) Provider {
	return Provider{Account: compatPrefix + configName}
}

// Lookup resolves a `smith auth` <provider> token to a Provider: the built-in
// "anthropic"/"openai" accounts, or an "openai-compatible:<name>" endpoint.
func Lookup(name string) (Provider, bool) {
	for _, p := range Builtins() {
		if p.Account == name {
			return p, true
		}
	}
	if strings.HasPrefix(name, compatPrefix) && len(name) > len(compatPrefix) {
		return Provider{Account: name}, true
	}
	return Provider{}, false
}

// Resolve returns the API key for p: the environment variable wins (the CI /
// headless escape hatch, D9), else the keychain. A missing key — no env var and
// nothing stored, or no keychain at all — returns "" with no error, so callers
// decide whether absence is fatal. Only an unexpected keychain failure propagates.
func Resolve(getenv func(string) string, store Store, p Provider) (string, error) {
	if getenv != nil && p.EnvVar != "" {
		if v := strings.TrimSpace(getenv(p.EnvVar)); v != "" {
			return v, nil
		}
	}
	secret, err := store.Get(p.Account)
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnavailable):
		return "", nil
	case err != nil:
		return "", err
	}
	return secret, nil
}

// Keyring is the production Store backed by the OS secret service via
// go-keyring (macOS Keychain, Linux Secret Service, Windows Credential Manager).
type Keyring struct{}

// secretServiceUnreachableSignatures are substrings of the plain errors
// go-keyring surfaces on a *supported* platform whose secret service is not
// actually reachable — the headless-Linux case AS-017 supports via env vars.
// go-keyring only maps a wholly unsupported platform to ErrUnsupportedPlatform;
// a missing D-Bus, absent `dbus-launch`, or unprovided `org.freedesktop.secrets`
// name fall through as opaque errors, so we recognize them by signature. The set
// is deliberately narrow: only "secret store unreachable" modes are downgraded to
// ErrUnavailable, so a genuine backend bug still propagates verbatim (AS-144).
var secretServiceUnreachableSignatures = []string{
	"dbus-launch",                   // exec: "dbus-launch": executable file not found
	"org.freedesktop.secrets",       // name was not provided by any .service files
	"the name org.freedesktop.dbus", // bus name not provided
	"cannot autolaunch d-bus",       // no X11 / no session bus to autolaunch
	"dbus_session_bus_address",      // session bus address unset
	"$display",                      // autolaunch needs $DISPLAY
	"connection refused",            // dial to the bus socket refused
	"dial unix",                     // dial to the bus socket failed (e.g. missing socket path)
	"dbus socket",                   // failed to open the D-Bus socket
}

// isSecretServiceUnreachable reports whether err is one of the recognized
// "no reachable secret service" failures that should classify as ErrUnavailable
// rather than leak a raw D-Bus/exec error to the user.
func isSecretServiceUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, sig := range secretServiceUnreachableSignatures {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// Get implements Store.
func (Keyring) Get(account string) (string, error) {
	secret, err := keyring.Get(Service, account)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", ErrNotFound
	case errors.Is(err, keyring.ErrUnsupportedPlatform), isSecretServiceUnreachable(err):
		return "", ErrUnavailable
	case err != nil:
		return "", fmt.Errorf("keychain get %q: %w", account, err)
	}
	return secret, nil
}

// Set implements Store.
func (Keyring) Set(account, secret string) error {
	err := keyring.Set(Service, account, secret)
	switch {
	case errors.Is(err, keyring.ErrUnsupportedPlatform), isSecretServiceUnreachable(err):
		return ErrUnavailable
	case err != nil:
		return fmt.Errorf("keychain set %q: %w", account, err)
	}
	return nil
}

// Remove implements Store.
func (Keyring) Remove(account string) error {
	err := keyring.Delete(Service, account)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, keyring.ErrUnsupportedPlatform), isSecretServiceUnreachable(err):
		return ErrUnavailable
	case err != nil:
		return fmt.Errorf("keychain delete %q: %w", account, err)
	}
	return nil
}
