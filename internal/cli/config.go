package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config resolves a setting through the D-CLI-6 precedence chain:
//
//	flag > project file > user file > SMITH_* env > built-in default
//
// A flag always wins; repo-pinned config (./.smith/config) outranks user config
// (~/.config/smith/config), which outranks SMITH_* env vars, which outrank the
// built-in default. Env sits *below* the project file on purpose: a checked-in
// repo policy stays reproducible regardless of ambient environment. Secrets are
// deliberately out of this chain (keychain/env per AS-017).
type Config struct {
	Flags    map[string]string   // highest precedence (explicit --flag overrides)
	Project  map[string]string   // ./.smith/config
	User     map[string]string   // ~/.config/smith/config
	Getenv   func(string) string // SMITH_* lookup; nil reads nothing
	Defaults map[string]string   // lowest precedence
}

// Get returns the resolved value for key and the source it came from
// ("flag", "project", "user", "env", "default"). ok is false when no layer sets
// it.
func (c Config) Get(key string) (value, source string, ok bool) {
	if v, has := c.Flags[key]; has {
		return v, "flag", true
	}
	if v, has := c.Project[key]; has {
		return v, "project", true
	}
	if v, has := c.User[key]; has {
		return v, "user", true
	}
	if c.Getenv != nil {
		if v := c.Getenv(EnvKey(key)); v != "" {
			return v, "env", true
		}
	}
	if v, has := c.Defaults[key]; has {
		return v, "default", true
	}
	return "", "", false
}

// EnvKey maps a config key to its environment variable: "model" → "SMITH_MODEL",
// "max-tokens" → "SMITH_MAX_TOKENS".
func EnvKey(key string) string {
	return "SMITH_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

// LoadConfigFile reads a `key = value` file (lines starting with # and blank
// lines are ignored; surrounding whitespace and matching quotes are trimmed). A
// missing file is not an error — it returns an empty map so an absent layer is
// just empty, never a failure.
func LoadConfigFile(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only handle

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, val, found := strings.Cut(text, "=")
		if !found {
			return nil, fmt.Errorf("%s:%d: not a key=value line: %q", path, line, text)
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"`)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveConfigValue upserts key=value into the file at path, creating parent
// directories and preserving the other entries. It rewrites the file from the
// merged map (sorted) — fine for the small config files this targets.
func SaveConfigValue(path, key, value string) error {
	// Guard the line format: a key with '=' or a newline, or a value with a
	// newline, would not survive the key=value round-trip and could corrupt the
	// rest of the file.
	if key == "" || strings.ContainsAny(key, "=\n\r") {
		return fmt.Errorf("invalid config key %q: must be non-empty and contain no '=' or newline", key)
	}
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("invalid config value for %q: must contain no newline", key)
	}
	cur, err := LoadConfigFile(path)
	if err != nil {
		return err
	}
	cur[key] = value

	keys := make([]string, 0, len(cur))
	for k := range cur {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, cur[k])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
