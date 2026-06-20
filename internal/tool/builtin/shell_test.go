package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// newShell builds a Shell rooted at a fresh temp dir. It pins the shell binary to
// /bin/sh so tests are deterministic regardless of the runner's $SHELL.
func newShell(t *testing.T, opts ...ShellOption) (*Shell, string) {
	t.Helper()
	root := t.TempDir()
	opts = append([]ShellOption{WithShellPath("/bin/sh")}, opts...)
	s, err := NewShell(root, opts...)
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	return s, root
}

// runShell invokes the shell tool directly with the given command.
func runShell(t *testing.T, s *Shell, command string) tool.Output {
	t.Helper()
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := s.Run(t.Context(), args)
	if err != nil {
		t.Fatalf("Run returned a Go error: %v", err)
	}
	return out
}

func TestShellDef(t *testing.T) {
	s, _ := newShell(t)
	def := s.Def()
	if def.Name != "shell" {
		t.Fatalf("name = %q, want shell", def.Name)
	}
	if def.Timeout <= s.timeout {
		t.Fatalf("advertised timeout %s should exceed the tool's own %s", def.Timeout, s.timeout)
	}
	// The arguments schema must expose "command" as required — the field the
	// permission model matches allow-rule patterns against.
	if !strings.Contains(string(def.InputSchema), `"command"`) || !strings.Contains(string(def.InputSchema), `"required"`) {
		t.Fatalf("input schema missing required command field: %s", def.InputSchema)
	}
}

func TestShellRunCapturesOutputAndExitCode(t *testing.T) {
	s, _ := newShell(t)
	out := runShell(t, s, "echo hello")
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Text)
	}
	if !strings.Contains(out.Text, "hello") {
		t.Fatalf("output missing command stdout: %q", out.Text)
	}
	if !strings.Contains(out.Text, "exit code 0") {
		t.Fatalf("output missing exit code: %q", out.Text)
	}
}

func TestShellNonZeroExitIsError(t *testing.T) {
	s, _ := newShell(t)
	out := runShell(t, s, "echo oops >&2; exit 3")
	if !out.IsError {
		t.Fatalf("non-zero exit should be an error result: %q", out.Text)
	}
	if !strings.Contains(out.Text, "exit code 3") {
		t.Fatalf("output missing exit code 3: %q", out.Text)
	}
	if !strings.Contains(out.Text, "oops") {
		t.Fatalf("stderr not captured: %q", out.Text)
	}
}

func TestShellInterleavesStdoutAndStderr(t *testing.T) {
	s, _ := newShell(t)
	out := runShell(t, s, "echo one; echo two >&2; echo three")
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("combined output missing %q: %q", want, out.Text)
		}
	}
}

func TestShellWorkingDirectoryIsRoot(t *testing.T) {
	s, root := newShell(t)
	// Proving cwd via a side effect avoids /tmp symlink mismatches that comparing
	// `pwd` output to root would hit on some platforms.
	out := runShell(t, s, "touch marker")
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); err != nil {
		t.Fatalf("command did not run in the project root: %v", err)
	}
}

func TestShellTimeoutKillsAndReports(t *testing.T) {
	s, _ := newShell(t, WithShellTimeout(100*time.Millisecond))
	start := time.Now()
	out := runShell(t, s, "sleep 5")
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("command was not killed promptly: ran for %s", elapsed)
	}
	if !out.IsError {
		t.Fatalf("timeout should be an error result: %q", out.Text)
	}
	if !strings.Contains(out.Text, "timed out") {
		t.Fatalf("output should report the timeout: %q", out.Text)
	}
}

func TestShellOutputCapTruncates(t *testing.T) {
	s, _ := newShell(t, WithMaxShellOutputBytes(64))
	out := runShell(t, s, "for i in $(seq 1 1000); do echo line-$i; done")
	if !strings.Contains(out.Text, "truncated at 64 bytes") {
		t.Fatalf("output should be marked truncated: %q", out.Text)
	}
	if len(out.Text) > 512 {
		t.Fatalf("truncated output should be small, got %d bytes", len(out.Text))
	}
}

func TestShellEmptyCommand(t *testing.T) {
	s, _ := newShell(t)
	out := runShell(t, s, "   ")
	if !out.IsError || !strings.Contains(out.Text, "command is required") {
		t.Fatalf("want required-command error, got IsError=%v text=%q", out.IsError, out.Text)
	}
}

func TestShellInvalidArguments(t *testing.T) {
	s, _ := newShell(t)
	out, err := s.Run(t.Context(), json.RawMessage(`{"command": 5}`))
	if err != nil {
		t.Fatalf("Run returned a Go error: %v", err)
	}
	if !out.IsError || !strings.Contains(out.Text, "invalid arguments") {
		t.Fatalf("want invalid-arguments error, got IsError=%v text=%q", out.IsError, out.Text)
	}
}

func TestShellParentCancellationAbortsTurn(t *testing.T) {
	s, _ := newShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: the surrounding turn is being abandoned
	args, _ := json.Marshal(map[string]string{"command": "echo hi"})
	_, err := s.Run(ctx, args)
	if err == nil {
		t.Fatal("a cancelled parent context should return a Go error, not a result")
	}
}

func TestShellNoOutput(t *testing.T) {
	s, _ := newShell(t)
	out := runShell(t, s, "true")
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	if !strings.Contains(out.Text, "(no output)") {
		t.Fatalf("empty output should be marked: %q", out.Text)
	}
}

// TestShellDenialReportedAsFeedback drives the tool through the Runtime with a
// denying permission policy: nothing executes, and the denial comes back as a
// model-readable error result rather than a turn-aborting Go error (AS-015 AC,
// AS-016).
func TestShellDenialReportedAsFeedback(t *testing.T) {
	s, root := newShell(t)
	reg := tool.NewRegistry()
	if err := reg.Register(s); err != nil {
		t.Fatalf("register shell: %v", err)
	}
	log := eventlog.New()
	deny := func(context.Context, tool.Call) tool.Decision {
		return tool.Denied("not approved")
	}
	rt := tool.NewRuntime(reg, log, tool.WithPermission(deny))

	args, _ := json.Marshal(map[string]string{"command": "touch should-not-exist"})
	call := schema.Block{
		Kind: schema.KindToolCall,
		Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{
			ToolUseID: "call-1",
			Name:      "shell",
			Arguments: args,
		},
	}
	result, err := rt.Execute(t.Context(), call)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.ToolResult == nil || !result.ToolResult.IsError {
		t.Fatalf("denied call should record an error tool_result: %+v", result.ToolResult)
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-exist")); err == nil {
		t.Fatal("denied command must not have executed")
	}
}

func TestCappedWriterTrimsPartialRune(t *testing.T) {
	// "é" is two bytes (0xC3 0xA9); a cap of 1 must drop the dangling lead byte.
	w := &cappedWriter{limit: 1}
	if _, err := w.Write([]byte("é")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := w.string(); got != "" {
		t.Fatalf("partial rune not trimmed: %q", got)
	}
	if !w.truncated {
		t.Fatal("writer should be marked truncated")
	}
}
