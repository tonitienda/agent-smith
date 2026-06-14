package permission

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if !reflect.DeepEqual(cfg, Config{}) {
		t.Fatalf("missing file = %+v, want zero Config", cfg)
	}
}

func TestLoadAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	body := `{
  "default_mode": "allowlist",
  "tools": {"shell": "ask"},
  "allow": [{"tool": "read"}, {"tool": "shell", "pattern": "git status*"}]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultMode != ModeAllowlist || cfg.Tools["shell"] != ModeAsk || len(cfg.Allow) != 2 {
		t.Fatalf("loaded config = %+v", cfg)
	}
}

func TestMergePrecedence(t *testing.T) {
	user := Config{
		DefaultMode: ModeAsk,
		Tools:       map[string]Mode{"shell": ModeAsk, "read": ModeAuto},
		Allow:       []Rule{{Tool: "read"}},
	}
	project := Config{
		DefaultMode: ModeAllowlist,
		Tools:       map[string]Mode{"shell": ModeAuto},
		Allow:       []Rule{{Tool: "shell", Pattern: "git status*"}, {Tool: "read"}}, // read dup
	}
	got := Merge(user, project)

	if got.DefaultMode != ModeAllowlist {
		t.Errorf("DefaultMode = %q, want project's allowlist", got.DefaultMode)
	}
	if got.Tools["shell"] != ModeAuto {
		t.Errorf("Tools[shell] = %q, want project's auto", got.Tools["shell"])
	}
	if got.Tools["read"] != ModeAuto {
		t.Errorf("Tools[read] = %q, want user's auto (untouched)", got.Tools["read"])
	}
	if len(got.Allow) != 2 { // read deduped, shell added
		t.Errorf("Allow = %+v, want 2 (deduped)", got.Allow)
	}

	// Merge must not mutate the inputs.
	if len(user.Allow) != 1 || len(project.Allow) != 2 {
		t.Errorf("Merge mutated an input: user=%+v project=%+v", user.Allow, project.Allow)
	}
}

func TestAppendRuleCreatesAndDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "permissions.json")
	rule := Rule{Tool: "shell", Pattern: "git status*"}

	if err := AppendRule(path, rule); err != nil {
		t.Fatalf("AppendRule (create): %v", err)
	}
	if err := AppendRule(path, rule); err != nil { // idempotent
		t.Fatalf("AppendRule (dup): %v", err)
	}
	if err := AppendRule(path, Rule{Tool: "read"}); err != nil {
		t.Fatalf("AppendRule (second): %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != 2 {
		t.Fatalf("Allow = %+v, want 2 (no duplicate)", cfg.Allow)
	}
}

func TestFilePersisterAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	persist := FilePersister(path)
	if err := persist(Rule{Tool: "write", Pattern: "docs/**"}); err != nil {
		t.Fatalf("persist: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0].Pattern != "docs/**" {
		t.Fatalf("persisted config = %+v", cfg)
	}
}

func TestDefaultPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	if got, want := UserConfigPath(), "/tmp/cfg/agent-smith/permissions.json"; got != want {
		t.Errorf("UserConfigPath = %q, want %q", got, want)
	}
	if got, want := ProjectConfigPath("/proj"), "/proj/.smith/permissions.json"; got != want {
		t.Errorf("ProjectConfigPath = %q, want %q", got, want)
	}
}
