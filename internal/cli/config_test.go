package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes body to path, creating parent directories.
func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func TestConfigPrecedence(t *testing.T) {
	// All five layers set "model"; precedence must pick flag first, then project,
	// user, env, default (D-CLI-6).
	full := Config{
		Flags:    map[string]string{"model": "flag-model"},
		Project:  map[string]string{"model": "project-model"},
		User:     map[string]string{"model": "user-model"},
		Getenv:   func(k string) string { return map[string]string{"SMITH_MODEL": "env-model"}[k] },
		Defaults: map[string]string{"model": "default-model"},
	}

	steps := []struct {
		name       string
		drop       func(*Config)
		wantValue  string
		wantSource string
	}{
		{"flag wins", func(c *Config) {}, "flag-model", "flag"},
		{"project next", func(c *Config) { c.Flags = nil }, "project-model", "project"},
		{"user next", func(c *Config) { c.Project = nil }, "user-model", "user"},
		{"env next", func(c *Config) { c.User = nil }, "env-model", "env"},
		{"default last", func(c *Config) { c.Getenv = nil }, "default-model", "default"},
	}

	cfg := full
	for _, s := range steps {
		s.drop(&cfg)
		v, src, ok := cfg.Get("model")
		if !ok {
			t.Fatalf("%s: Get returned ok=false", s.name)
		}
		if v != s.wantValue || src != s.wantSource {
			t.Errorf("%s: Get = (%q, %q), want (%q, %q)", s.name, v, src, s.wantValue, s.wantSource)
		}
	}

	// With every layer dropped, an unset key reports ok=false rather than "".
	empty := Config{}
	if _, _, ok := empty.Get("model"); ok {
		t.Error("empty config Get returned ok=true for unset key")
	}
}

func TestEnvKeyMapping(t *testing.T) {
	cases := map[string]string{"model": "SMITH_MODEL", "max-tokens": "SMITH_MAX_TOKENS"}
	for in, want := range cases {
		if got := EnvKey(in); got != want {
			t.Errorf("EnvKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config")

	// Missing file → empty map, no error.
	m, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile(missing): %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("missing file = %v, want empty", m)
	}

	if err := SaveConfigValue(path, "model", "claude-opus-4-8"); err != nil {
		t.Fatalf("SaveConfigValue: %v", err)
	}
	// A second key must not clobber the first.
	if err := SaveConfigValue(path, "output", "json"); err != nil {
		t.Fatalf("SaveConfigValue: %v", err)
	}

	m, err = LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if m["model"] != "claude-opus-4-8" || m["output"] != "json" {
		t.Errorf("round-trip = %v, want model + output preserved", m)
	}
}

func TestSaveConfigValueRejectsCorruptingInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	bad := []struct {
		name, key, value string
	}{
		{"empty key", "", "v"},
		{"key with equals", "a=b", "v"},
		{"key with newline", "a\nb", "v"},
		{"value with newline", "k", "line1\nline2"},
	}
	for _, tc := range bad {
		if err := SaveConfigValue(path, tc.key, tc.value); err == nil {
			t.Errorf("%s: SaveConfigValue accepted corrupting input", tc.name)
		}
	}
	// A rejected write must not have created the file (no partial state).
	if _, err := LoadConfigFile(path); err != nil {
		t.Fatalf("LoadConfigFile after rejected writes: %v", err)
	}
	// A value containing '=' is fine — only the first '=' splits on read.
	if err := SaveConfigValue(path, "url", "k=v&x=y"); err != nil {
		t.Fatalf("SaveConfigValue(value with '='): %v", err)
	}
	m, _ := LoadConfigFile(path)
	if m["url"] != "k=v&x=y" {
		t.Errorf("value with '=' round-trip = %q, want k=v&x=y", m["url"])
	}
}

func TestConfigFileIgnoresCommentsAndQuotes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	const body = "# a comment\n\nmodel = \"claude-opus-4-8\"\noutput=json\n"
	if err := writeFile(path, body); err != nil {
		t.Fatal(err)
	}
	m, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if m["model"] != "claude-opus-4-8" {
		t.Errorf("quoted value = %q, want unquoted", m["model"])
	}
	if m["output"] != "json" {
		t.Errorf("output = %q, want json", m["output"])
	}
}
