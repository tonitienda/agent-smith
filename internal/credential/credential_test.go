package credential

import (
	"errors"
	"testing"
)

// fakeStore is an in-memory Store so credential tests never touch a host
// keychain (AS-017 AC: lookup goes through a narrow internal interface).
type fakeStore struct {
	keys        map[string]string
	unavailable bool
}

func newFake() *fakeStore { return &fakeStore{keys: map[string]string{}} }

func (f *fakeStore) Get(account string) (string, error) {
	if f.unavailable {
		return "", ErrUnavailable
	}
	v, ok := f.keys[account]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *fakeStore) Set(account, secret string) error {
	if f.unavailable {
		return ErrUnavailable
	}
	f.keys[account] = secret
	return nil
}

func (f *fakeStore) Remove(account string) error {
	if f.unavailable {
		return ErrUnavailable
	}
	if _, ok := f.keys[account]; !ok {
		return ErrNotFound
	}
	delete(f.keys, account)
	return nil
}

func TestResolveEnvWinsOverKeychain(t *testing.T) {
	store := newFake()
	store.keys[Anthropic.Account] = "from-keychain"
	getenv := func(k string) string {
		if k == Anthropic.EnvVar {
			return "  from-env  " // trimmed
		}
		return ""
	}
	got, err := Resolve(getenv, store, Anthropic)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-env" {
		t.Fatalf("env should win and be trimmed: got %q", got)
	}
}

func TestResolveFallsBackToKeychain(t *testing.T) {
	store := newFake()
	store.keys[OpenAI.Account] = "stored"
	got, err := Resolve(func(string) string { return "" }, store, OpenAI)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "stored" {
		t.Fatalf("want stored key, got %q", got)
	}
}

func TestResolveMissingIsEmptyNotError(t *testing.T) {
	for name, store := range map[string]*fakeStore{
		"not found":   newFake(),
		"unavailable": {keys: map[string]string{}, unavailable: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Resolve(func(string) string { return "" }, store, Anthropic)
			if err != nil {
				t.Fatalf("Resolve should swallow %s: %v", name, err)
			}
			if got != "" {
				t.Fatalf("want empty key, got %q", got)
			}
		})
	}
}

func TestResolvePropagatesUnexpectedError(t *testing.T) {
	boom := errors.New("boom")
	store := errStore{err: boom}
	if _, err := Resolve(func(string) string { return "" }, store, Anthropic); !errors.Is(err, boom) {
		t.Fatalf("want boom propagated, got %v", err)
	}
}

type errStore struct{ err error }

func (e errStore) Get(string) (string, error) { return "", e.err }
func (e errStore) Set(string, string) error   { return e.err }
func (e errStore) Remove(string) error        { return e.err }

func TestLookup(t *testing.T) {
	cases := []struct {
		name    string
		want    string
		wantOK  bool
		wantEnv string
	}{
		{"anthropic", "anthropic", true, "ANTHROPIC_API_KEY"},
		{"openai", "openai", true, "OPENAI_API_KEY"},
		{"openai-compatible:groq", "openai-compatible:groq", true, ""},
		{"openai-compatible:", "", false, ""},
		{"bogus", "", false, ""},
	}
	for _, c := range cases {
		p, ok := Lookup(c.name)
		if ok != c.wantOK {
			t.Fatalf("Lookup(%q) ok=%v want %v", c.name, ok, c.wantOK)
		}
		if ok && (p.Account != c.want || p.EnvVar != c.wantEnv) {
			t.Fatalf("Lookup(%q) = %+v, want account %q env %q", c.name, p, c.want, c.wantEnv)
		}
	}
}

// TestIsSecretServiceUnreachable covers the AS-144 classification: the opaque
// errors go-keyring returns on a supported platform whose Secret Service is not
// reachable must be recognized (so callers reach the ErrUnavailable / env-hint
// path), while genuinely unexpected errors must propagate verbatim.
func TestIsSecretServiceUnreachable(t *testing.T) {
	unreachable := []string{
		`exec: "dbus-launch": executable file not found in $PATH`,
		`The name org.freedesktop.secrets was not provided by any .service files`,
		`Cannot autolaunch D-Bus without X11 $DISPLAY`,
		`dial unix /run/user/0/bus: connect: connection refused`,
		`failed to open dbus socket: no such file or directory`,
		`$DBUS_SESSION_BUS_ADDRESS is not set`,
	}
	for _, msg := range unreachable {
		if !isSecretServiceUnreachable(errors.New(msg)) {
			t.Errorf("isSecretServiceUnreachable(%q) = false, want true", msg)
		}
	}

	expected := []string{
		"keyring item too large",
		"permission denied writing to keyring",
		"some genuinely unexpected backend failure",
	}
	for _, msg := range expected {
		if isSecretServiceUnreachable(errors.New(msg)) {
			t.Errorf("isSecretServiceUnreachable(%q) = true, want false", msg)
		}
	}

	if isSecretServiceUnreachable(nil) {
		t.Error("isSecretServiceUnreachable(nil) = true, want false")
	}
}
