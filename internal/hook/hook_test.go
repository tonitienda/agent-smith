package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/config"
)

// run fires a single-hook Set built from spec against payload and returns the
// Outcome, failing the test if compilation dropped the spec.
func run(t *testing.T, spec Spec, p Payload) Outcome {
	t.Helper()
	set, warns, err := Compile([]Spec{spec})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	return set.Run(context.Background(), p)
}

func TestAllowOnCleanExit(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Command: "exit 0"},
		Payload{Event: PreToolUse, Tool: "shell"})
	if out.Blocked {
		t.Fatalf("expected allow, got blocked: %q", out.Reason)
	}
	if out.Input != nil || len(out.Annotations) != 0 || len(out.Warnings) != 0 {
		t.Fatalf("expected empty outcome, got %+v", out)
	}
}

func TestBlockViaExit2(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Command: "echo 'no shell allowed' >&2; exit 2"},
		Payload{Event: PreToolUse, Tool: "shell"})
	if !out.Blocked {
		t.Fatal("expected blocked")
	}
	if !strings.Contains(out.Reason, "no shell allowed") {
		t.Fatalf("reason should carry stderr, got %q", out.Reason)
	}
}

func TestBlockViaDecisionJSON(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Command: `echo '{"decision":"block","reason":"policy"}'`},
		Payload{Event: PreToolUse, Tool: "shell"})
	if !out.Blocked || out.Reason != "policy" {
		t.Fatalf("expected block with reason 'policy', got %+v", out)
	}
}

func TestModifyInput(t *testing.T) {
	// A modify hook rewrites the tool arguments via {"input":…}.
	out := run(t, Spec{Event: string(PreToolUse), Command: `echo '{"input":{"path":"/safe"}}'`},
		Payload{Event: PreToolUse, Tool: "read", Input: json.RawMessage(`{"path":"/etc/shadow"}`)})
	if out.Blocked {
		t.Fatalf("modify should not block: %q", out.Reason)
	}
	if string(out.Input) != `{"path":"/safe"}` {
		t.Fatalf("input not rewritten, got %s", out.Input)
	}
}

func TestPayloadReachesHookStdin(t *testing.T) {
	// The hook echoes the tool name from the payload back as a block reason,
	// proving the JSON payload arrived on stdin.
	cmd := `tool=$(cat); echo "{\"decision\":\"block\",\"reason\":\"saw $(echo "$tool" | sed 's/.*\"tool\":\"//;s/\".*//')\"}"`
	out := run(t, Spec{Event: string(PreToolUse), Command: cmd},
		Payload{Event: PreToolUse, Tool: "shell"})
	if !strings.Contains(out.Reason, "saw shell") {
		t.Fatalf("payload not seen on stdin, reason=%q", out.Reason)
	}
}

func TestAnnotation(t *testing.T) {
	out := run(t, Spec{Event: string(PostToolUse), Command: `echo '{"annotation":"ran a tool"}'`},
		Payload{Event: PostToolUse, Tool: "shell"})
	if out.Blocked {
		t.Fatal("annotation should not block")
	}
	if len(out.Annotations) != 1 || out.Annotations[0] != "ran a tool" {
		t.Fatalf("annotation not captured: %+v", out.Annotations)
	}
}

func TestTimeoutFailClosedBlocks(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Command: "sleep 5", Timeout: "50ms"},
		Payload{Event: PreToolUse, Tool: "shell"})
	if !out.Blocked {
		t.Fatal("fail-closed timeout should block")
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "timed out") {
		t.Fatalf("expected timeout warning, got %v", out.Warnings)
	}
}

func TestTimeoutFailOpenContinues(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Command: "sleep 5", Timeout: "50ms", FailOpen: true},
		Payload{Event: PreToolUse, Tool: "shell"})
	if out.Blocked {
		t.Fatal("fail-open timeout should continue")
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "timed out") {
		t.Fatalf("expected timeout warning, got %v", out.Warnings)
	}
}

func TestFailureFailClosedBlocks(t *testing.T) {
	// A non-zero exit outside the block convention (exit 1) is a failure.
	out := run(t, Spec{Event: string(PreToolUse), Command: "echo boom >&2; exit 1"},
		Payload{Event: PreToolUse, Tool: "shell"})
	if !out.Blocked {
		t.Fatal("fail-closed failure should block")
	}
	if !strings.Contains(out.Reason, "boom") {
		t.Fatalf("failure reason should carry stderr, got %q", out.Reason)
	}
}

func TestFailureFailOpenContinues(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Command: "exit 1", FailOpen: true},
		Payload{Event: PreToolUse, Tool: "shell"})
	if out.Blocked {
		t.Fatal("fail-open failure should continue")
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("expected one warning, got %v", out.Warnings)
	}
}

func TestMatcherFiltersByToolName(t *testing.T) {
	spec := Spec{Event: string(PreToolUse), Matcher: "shell", Command: "exit 2"}
	// Matches "shell" → blocks.
	if out := run(t, spec, Payload{Event: PreToolUse, Tool: "shell"}); !out.Blocked {
		t.Fatal("matcher 'shell' should fire on tool 'shell'")
	}
	// Does not match "read" → no hook runs, allow.
	if out := run(t, spec, Payload{Event: PreToolUse, Tool: "read"}); out.Blocked {
		t.Fatal("matcher 'shell' should not fire on tool 'read'")
	}
}

func TestMatcherGlob(t *testing.T) {
	out := run(t, Spec{Event: string(PreToolUse), Matcher: "file_*", Command: "exit 2"},
		Payload{Event: PreToolUse, Tool: "file_write"})
	if !out.Blocked {
		t.Fatal("glob 'file_*' should match 'file_write'")
	}
}

func TestChainFirstBlockWins(t *testing.T) {
	set := New(map[Event][]hook{
		PreToolUse: {
			{event: PreToolUse, command: `echo '{"annotation":"first"}'`, timeout: time.Second},
			{event: PreToolUse, command: "exit 2", timeout: time.Second},
			{event: PreToolUse, command: `echo '{"annotation":"third"}'`, timeout: time.Second},
		},
	})
	out := set.Run(context.Background(), Payload{Event: PreToolUse, Tool: "shell"})
	if !out.Blocked {
		t.Fatal("expected block from second hook")
	}
	// First hook's annotation is kept; the third never runs.
	if len(out.Annotations) != 1 || out.Annotations[0] != "first" {
		t.Fatalf("chain should stop at the block, got annotations %v", out.Annotations)
	}
}

func TestChainModificationsFeedForward(t *testing.T) {
	// First hook rewrites input to {"n":1}; second echoes back its received input
	// as the new input, proving it saw the first hook's edit.
	set := New(map[Event][]hook{
		PreToolUse: {
			{event: PreToolUse, command: `echo '{"input":{"n":1}}'`, timeout: time.Second},
			{event: PreToolUse, command: `in=$(cat); echo "{\"input\":$(echo "$in" | sed 's/.*\"input\"://;s/,\"result\".*//;s/}}$/}/')}"`, timeout: time.Second},
		},
	})
	out := set.Run(context.Background(), Payload{Event: PreToolUse, Tool: "x", Input: json.RawMessage(`{"n":0}`)})
	if out.Blocked {
		t.Fatalf("unexpected block: %q", out.Reason)
	}
	if !strings.Contains(string(out.Input), `"n":1`) {
		t.Fatalf("second hook did not see first hook's modification, got %s", out.Input)
	}
}

func TestCompileWarnings(t *testing.T) {
	_, warns, err := Compile([]Spec{
		{Event: "bogus", Command: "true"},
		{Event: string(PreToolUse)},                                 // missing command
		{Event: string(PreToolUse), Command: "true", Timeout: "x"},  // bad timeout
		{Event: string(PreToolUse), Command: "true", Matcher: "[a"}, // bad glob
		{Event: string(PreToolUse), Command: "true", Timeout: "1s"}, // ok
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(warns) != 4 {
		t.Fatalf("expected 4 warnings, got %d: %v", len(warns), warns)
	}
}

// loadConfig builds a real *config.Config from a JSON document written to a temp
// file — the production read path (a layered config over a project file) — so
// Load is exercised against the genuine Decode collaborator rather than a
// hand-written double. body is the full config object (e.g. `{"hooks":[...]}`);
// an empty body yields a config with no `hooks` key.
func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if body == "" {
		body = "{}"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	layer, err := config.FileLayer("project", path)
	if err != nil {
		t.Fatalf("FileLayer: %v", err)
	}
	return config.New(layer)
}

func TestLoadFromConfig(t *testing.T) {
	cfg := loadConfig(t, `{"hooks":[{"event":"session-start","command":"true"}]}`)
	set, warns, err := Load(cfg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if !set.Has(SessionStart) {
		t.Fatal("expected a session-start hook")
	}
}

func TestLoadMissingHooksKey(t *testing.T) {
	set, warns, err := Load(loadConfig(t, ""))
	if err != nil || len(warns) != 0 {
		t.Fatalf("missing key should be clean: err=%v warns=%v", err, warns)
	}
	if set.Has(SessionStart) {
		t.Fatal("expected empty set")
	}
}

func TestLoadNilConfig(t *testing.T) {
	set, warns, err := Load(nil)
	if err != nil || len(warns) != 0 {
		t.Fatalf("nil config should be clean: err=%v warns=%v", err, warns)
	}
	if set.Has(SessionStart) {
		t.Fatal("expected empty set")
	}
}

func TestNilSetIsInert(t *testing.T) {
	var s *Set
	if s.Has(PreToolUse) {
		t.Fatal("nil set has nothing")
	}
	if out := s.Run(context.Background(), Payload{Event: PreToolUse}); out.Blocked {
		t.Fatal("nil set never blocks")
	}
}
