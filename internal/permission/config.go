package permission

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Rule is one allow-list entry: a tool name plus an optional pattern matched
// against the call's subject (see MatchStyle). Tool may be "*" to match any
// tool; an empty Pattern (or "*") matches any call of the tool regardless of
// arguments. Examples:
//
//	{Tool: "shell", Pattern: "git status*"}  // any "git status …" command
//	{Tool: "read"}                            // any read
//	{Tool: "write", Pattern: "docs/**"}       // writes anywhere under docs/
type Rule struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern,omitempty"`
}

func (r Rule) isZero() bool { return r.Tool == "" && r.Pattern == "" }

// Config is the serialized permission policy. It is read from layered files
// (user then project, merged) and is intentionally small and additive: new
// concepts become new optional fields (Decision Log D2), so an older binary
// ignores fields it does not know and a newer file still loads in an older one.
type Config struct {
	// DefaultMode is the session-wide mode for tools without an override. An
	// empty or unknown value resolves to ModeAsk at decision time.
	DefaultMode Mode `json:"default_mode,omitempty"`
	// Tools overrides the mode for specific tools by name.
	Tools map[string]Mode `json:"tools,omitempty"`
	// Allow is the allow-list consulted under ModeAllowlist (and grown by "always
	// allow this").
	Allow []Rule `json:"allow,omitempty"`
}

// clone returns a deep copy so a Policy can mutate its Allow list (remembered
// rules) without touching the caller's Config.
func (c Config) clone() Config {
	out := Config{DefaultMode: c.DefaultMode}
	if len(c.Tools) > 0 {
		out.Tools = make(map[string]Mode, len(c.Tools))
		for k, v := range c.Tools {
			out.Tools[k] = v
		}
	}
	if len(c.Allow) > 0 {
		out.Allow = append([]Rule(nil), c.Allow...)
	}
	return out
}

// Merge layers project config over user config: the project's DefaultMode wins
// when set, per-tool overrides merge with the project's winning per key, and the
// allow-lists concatenate (user rules first, then project). The result is a new
// Config; the inputs are not modified. This is the user→project precedence the
// rest of the config story (AS-031) will follow.
func Merge(user, project Config) Config {
	out := user.clone()

	if project.DefaultMode != "" {
		out.DefaultMode = project.DefaultMode
	}
	if len(project.Tools) > 0 {
		if out.Tools == nil {
			out.Tools = make(map[string]Mode, len(project.Tools))
		}
		for k, v := range project.Tools {
			out.Tools[k] = v
		}
	}
	for _, r := range project.Allow {
		if !containsRule(out.Allow, r) {
			out.Allow = append(out.Allow, r)
		}
	}
	return out
}

// containsRule reports whether rules already holds r (exact tool+pattern), so
// merging and remembering stay idempotent.
func containsRule(rules []Rule, r Rule) bool {
	for _, existing := range rules {
		if existing == r {
			return true
		}
	}
	return false
}

// Load reads a permission Config from a JSON file. A missing file is not an
// error: it returns the zero Config, so an absent user or project file simply
// contributes nothing to the merge.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("permission: read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("permission: parse %s: %w", path, err)
	}
	return cfg, nil
}

// LoadLayered loads the user and project config files and merges them
// (project over user). Either path may be empty (skipped) or missing.
func LoadLayered(userPath, projectPath string) (Config, error) {
	var user, project Config
	var err error
	if userPath != "" {
		if user, err = Load(userPath); err != nil {
			return Config{}, err
		}
	}
	if projectPath != "" {
		if project, err = Load(projectPath); err != nil {
			return Config{}, err
		}
	}
	return Merge(user, project), nil
}

// configFileName is the permission config file name at both the user and project
// layers. The full config system (AS-031) will own broader layering; this keeps
// AS-016 self-contained with a stable, predictable location.
const configFileName = "permissions.json"

// UserConfigPath returns the user-level permission config path,
// $XDG_CONFIG_HOME/agent-smith/permissions.json (falling back to
// ~/.config/agent-smith/…). It returns "" if neither can be determined.
func UserConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "agent-smith", configFileName)
}

// ProjectConfigPath returns the project-level permission config path under
// root: <root>/.smith/permissions.json. This is also where "always allow this"
// rules are appended (see FilePersister).
func ProjectConfigPath(root string) string {
	return filepath.Join(root, ".smith", configFileName)
}

// FilePersister returns a Persister that appends remembered rules to the file at
// path (typically ProjectConfigPath). The file and its parent directory are
// created on first write.
func FilePersister(path string) Persister {
	return func(r Rule) error { return AppendRule(path, r) }
}

// AppendRule adds r to the allow-list of the config file at path, creating the
// file (and its directory) if absent and leaving other fields untouched. A rule
// already present is a no-op, so repeated "always allow" of the same action does
// not duplicate it. The write is atomic (temp file + rename) so a crash cannot
// corrupt the config.
func AppendRule(path string, r Rule) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	if containsRule(cfg.Allow, r) {
		return nil
	}
	cfg.Allow = append(cfg.Allow, r)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("permission: encode config: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("permission: create config dir: %w", err)
	}
	return atomicWrite(path, data, 0o644)
}

// atomicWrite writes data to path via a sibling temp file and a rename, so an
// interrupted write leaves any existing config intact rather than half-written.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".permissions-*")
	if err != nil {
		return fmt.Errorf("permission: create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("permission: write temp config: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("permission: chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("permission: close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("permission: replace config: %w", err)
	}
	return nil
}

// normalizeMode lowercases and trims a mode string read from config so minor
// formatting differences ("Auto", " auto ") still resolve. It is applied lazily
// at validity checks rather than at load, keeping Load a pure decode.
func normalizeMode(m Mode) Mode { return Mode(strings.ToLower(strings.TrimSpace(string(m)))) }
