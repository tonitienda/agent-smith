package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/credential"
)

type fakeCreds struct {
	keys        map[string]string
	unavailable bool
}

func (f *fakeCreds) Get(a string) (string, error) {
	if f.unavailable {
		return "", credential.ErrUnavailable
	}
	v, ok := f.keys[a]
	if !ok {
		return "", credential.ErrNotFound
	}
	return v, nil
}

func (f *fakeCreds) Set(a, s string) error {
	if f.unavailable {
		return credential.ErrUnavailable
	}
	f.keys[a] = s
	return nil
}

func (f *fakeCreds) Remove(a string) error {
	if f.unavailable {
		return credential.ErrUnavailable
	}
	if _, ok := f.keys[a]; !ok {
		return credential.ErrNotFound
	}
	delete(f.keys, a)
	return nil
}

// withFakeCreds swaps authStore for a fake for the duration of a test.
func withFakeCreds(t *testing.T, f *fakeCreds) {
	t.Helper()
	prev := authStore
	authStore = f
	t.Cleanup(func() { authStore = prev })
}

func newCtx(args ...string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &cli.Context{Args: args, Stdout: &out, Stderr: &errb, Stdin: strings.NewReader("")}, &out, &errb
}

func TestAuthStatusReportsSources(t *testing.T) {
	withFakeCreds(t, &fakeCreds{keys: map[string]string{"openai": "x"}})
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	c, out, _ := newCtx()
	if err := authStatus(c); err != nil {
		t.Fatalf("authStatus: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "openai\tset (keychain)") {
		t.Errorf("openai should report keychain; got %q", got)
	}
	if !strings.Contains(got, "anthropic\tnot set") || !strings.Contains(got, "ANTHROPIC_API_KEY") {
		t.Errorf("anthropic should report not-set with env hint; got %q", got)
	}
}

func TestAuthStatusEnvWins(t *testing.T) {
	withFakeCreds(t, &fakeCreds{keys: map[string]string{}})
	t.Setenv("ANTHROPIC_API_KEY", "live")

	c, out, _ := newCtx("anthropic")
	if err := authStatus(c); err != nil {
		t.Fatalf("authStatus: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "set (env ANTHROPIC_API_KEY)") {
		t.Errorf("want env source, got %q", got)
	}
}

func TestAuthSetStoresPipedKey(t *testing.T) {
	f := &fakeCreds{keys: map[string]string{}}
	withFakeCreds(t, f)

	c, _, _ := newCtx("openai")
	c.Stdin = strings.NewReader("  sk-test\n")
	if err := authSet(c); err != nil {
		t.Fatalf("authSet: %v", err)
	}
	if f.keys["openai"] != "sk-test" {
		t.Fatalf("key not stored trimmed: %q", f.keys["openai"])
	}
}

func TestAuthSetEmptyIsUsageError(t *testing.T) {
	withFakeCreds(t, &fakeCreds{keys: map[string]string{}})
	c, _, _ := newCtx("openai")
	c.Stdin = strings.NewReader("   \n")
	if err := authSet(c); err == nil {
		t.Fatal("want error on empty key")
	}
}

func TestAuthSetUnavailableNamesEnvVar(t *testing.T) {
	withFakeCreds(t, &fakeCreds{keys: map[string]string{}, unavailable: true})
	c, _, _ := newCtx("anthropic")
	c.Stdin = strings.NewReader("sk-test")
	err := authSet(c)
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("want actionable error naming the env var, got %v", err)
	}
}

func TestAuthRemoveMissing(t *testing.T) {
	withFakeCreds(t, &fakeCreds{keys: map[string]string{}})
	c, _, _ := newCtx("openai")
	if err := authRemove(c); err == nil || !strings.Contains(err.Error(), "no stored key") {
		t.Fatalf("want no-stored-key error, got %v", err)
	}
}

func TestAuthUnknownProvider(t *testing.T) {
	withFakeCreds(t, &fakeCreds{keys: map[string]string{}})
	c, _, _ := newCtx("bogus")
	if err := authStatus(c); err == nil {
		t.Fatal("want unknown-provider usage error")
	}
}
