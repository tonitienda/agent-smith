package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tonitienda/agent-smith/internal/tool"
)

// Shell is the command-execution tool (AS-015, PRD §7.2): it runs a command line
// through a shell from the session project root and records the command's
// combined stdout/stderr, exit code, and duration as a tool_result the model can
// react to. It implements tool.Tool, so it plugs into the tool.Runtime like any
// other tool and inherits its argument validation, permission gate (AS-016),
// output truncation, and event-log provenance.
//
// Per Decision D9, V1 ships no OS-level sandbox: the security boundary is the
// permission model (AS-016) plus the documented posture — "Agent Smith runs with
// your privileges in your environment; you approve actions" (see
// docs/SECURITY.md). Every call still passes through the Runtime's permission
// check before Run is reached; the tool itself never bypasses it.
//
// The working directory is the project root for every call; a persistent cwd
// that survives across calls (so one command's `cd` is seen by the next) is not a
// V1 requirement — a command that needs a subdirectory says so itself (e.g.
// `cd sub && make`). Shell is safe for concurrent use: it holds only immutable
// configuration after construction, so the Runtime may run several calls against
// one Shell value at once (AS-019).
type Shell struct {
	root           string
	shellPath      string
	timeout        time.Duration
	maxOutputBytes int
}

// DefaultShellTimeout is the wall-clock budget a command gets before Shell kills
// it and reports the timeout to the model. It is the tool's own bound; the
// Runtime applies an independent, slightly larger per-tool budget as a backstop
// (see Def).
const DefaultShellTimeout = 60 * time.Second

// shellTimeoutGrace is added to the tool's own timeout when it advertises a
// per-tool budget to the Runtime, so the tool's internal deadline always fires
// first and produces the precise "timed out … and was killed" result rather than
// the Runtime's generic timeout message.
const shellTimeoutGrace = 5 * time.Second

// DefaultMaxShellOutputBytes caps how much combined output Shell buffers before
// it stops storing and marks the result truncated, so a runaway command cannot
// exhaust memory. The Runtime applies a second, independent cap to the recorded
// tool_result (tool.DefaultMaxResultBytes); this one bounds the tool's own
// buffer, mirroring the read tool's per-read cap.
const DefaultMaxShellOutputBytes = 64 * 1024

// ShellOption configures a Shell.
type ShellOption func(*Shell)

// WithShellTimeout overrides the per-command timeout. A non-positive d is
// ignored.
func WithShellTimeout(d time.Duration) ShellOption {
	return func(s *Shell) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// WithMaxShellOutputBytes overrides the combined-output byte cap. A non-positive
// n is ignored.
func WithMaxShellOutputBytes(n int) ShellOption {
	return func(s *Shell) {
		if n > 0 {
			s.maxOutputBytes = n
		}
	}
}

// WithShellPath overrides the shell binary used to run commands (default: $SHELL,
// falling back to /bin/sh). Commands are passed as a single argument after -c. An
// empty path is ignored.
func WithShellPath(path string) ShellOption {
	return func(s *Shell) {
		if strings.TrimSpace(path) != "" {
			s.shellPath = path
		}
	}
}

// NewShell builds a Shell rooted at root, which is resolved to an absolute path
// so the working directory is stable regardless of the process working
// directory.
func NewShell(root string, opts ...ShellOption) (*Shell, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("builtin: resolve shell root %q: %w", root, err)
	}
	s := &Shell{
		root:           filepath.Clean(abs),
		shellPath:      defaultShellPath(),
		timeout:        DefaultShellTimeout,
		maxOutputBytes: DefaultMaxShellOutputBytes,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// defaultShellPath honors the user's login shell ($SHELL) so commands run in the
// environment they expect, falling back to /bin/sh, which is present on macOS and
// Linux (the V1 platforms).
func defaultShellPath() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// Def describes the shell tool to the model. The arguments object carries a
// single required "command" field — the name the permission model matches
// allow-rule patterns against (see internal/permission). The advertised Timeout
// is the tool's own budget plus a grace margin, so the Runtime's per-tool bound
// only trips if the tool's own deadline handling somehow fails to.
func (s *Shell) Def() tool.Def {
	return tool.Def{
		Name: "shell",
		Description: "Run a command line through the shell from the project root and capture its " +
			"combined stdout and stderr, exit code, and duration. The working directory is the " +
			"project root for every call and does not persist between calls, so a command that needs " +
			"a subdirectory must change into it itself (e.g. `cd sub && make`). A command that exceeds " +
			"the timeout is killed and reported as timed out.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["command"],
  "properties": {
    "command": {"type": "string", "description": "The command line to run via the shell."}
  }
}`),
		Timeout: s.timeout + shellTimeoutGrace,
	}
}

// Run executes the command and returns its combined output, exit code, and
// duration as a model-readable result. A non-zero exit, a timeout, or a binary
// that fails to start become an error result the model can react to (IsError
// set, nil Go error); only cancellation of the surrounding turn returns a Go
// error, abandoning the turn without recording a result.
func (s *Shell) Run(ctx context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if strings.TrimSpace(in.Command) == "" {
		return errResult("command is required"), nil
	}

	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, s.shellPath, "-c", in.Command)
	cmd.Dir = s.root
	// Pointing Stdout and Stderr at the same writer makes os/exec share one pipe
	// for both, so the captured output is interleaved in the order it was written.
	out := &cappedWriter{limit: s.maxOutputBytes}
	cmd.Stdout = out
	cmd.Stderr = out
	// Run the command in its own process group and kill the whole group on timeout
	// or cancellation (configureProcessGroup), so a shell's children (the actual
	// `sleep`, compiler, etc.) die with it. Without this, exec only signals the
	// direct shell process and a surviving grandchild keeps the output pipe open,
	// blocking Wait until it exits on its own. WaitDelay is the stdlib backstop:
	// if a straggler still holds the pipe, it force-closes I/O so Run returns.
	configureProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	// The surrounding turn was cancelled (not our own timeout): abandon it so the
	// loop can stop, recording nothing.
	if ctx.Err() != nil {
		return tool.Output{}, ctx.Err()
	}

	body := out.string()
	truncated := out.truncated

	// Our own deadline elapsed: exec.CommandContext killed the process.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		status := fmt.Sprintf("timed out after %s and was killed · %s", s.timeout, roundDuration(elapsed))
		return tool.Output{Text: s.format(status, body, truncated), IsError: true}, nil
	}

	exitCode := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(runErr, &exitErr):
		exitCode = exitErr.ExitCode()
	case runErr != nil:
		// The command never ran (e.g. the shell binary is missing): a tool-level
		// failure the model should see, not a turn-aborting Go error.
		return errResult("could not run command: %v", runErr), nil
	}

	status := fmt.Sprintf("exit code %d · %s", exitCode, roundDuration(elapsed))
	return tool.Output{Text: s.format(status, body, truncated), IsError: exitCode != 0}, nil
}

// format renders the model-facing result: a status header line, the command's
// combined output verbatim, and an explicit marker when the output was capped.
// The body is reproduced exactly — including any trailing blank lines, which can
// be meaningful — so only the formatting newlines this function adds are managed.
func (s *Shell) format(status, body string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", status)
	if body == "" {
		b.WriteString("(no output)")
	} else {
		b.WriteString(body)
	}
	if truncated {
		if body != "" && !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[output truncated at %d bytes]", s.maxOutputBytes)
	}
	return b.String()
}

// roundDuration rounds a duration to a readable precision for the status line.
func roundDuration(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(10 * time.Millisecond)
}

// cappedWriter buffers writes up to a byte limit and records whether anything
// beyond it was dropped. It always reports a full write so the child process is
// never short-write-failed (which on a pipe would surface as SIGPIPE); the excess
// is simply discarded and flagged. A non-positive limit buffers everything.
//
// In this tool stdout and stderr are the same writer value, so os/exec shares a
// single pipe and one copier goroutine — writes never overlap. The mutex makes
// that safety explicit and keeps the writer correct even if it is later handed a
// pair of distinct streams that would be copied concurrently.
type cappedWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.limit <= 0 {
		w.buf.Write(p)
		return len(p), nil
	}
	if room := w.limit - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

// string returns the buffered output, trimming a trailing partial UTF-8 rune
// when a write was cut mid-rune at the cap so the result is always valid UTF-8
// (avoiding JSON-encode or TUI-render errors downstream). Callers invoke it after
// the command has exited, when no writes remain in flight.
func (w *cappedWriter) string() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.buf.String()
	if !w.truncated {
		return s
	}
	for i := 0; i < utf8.UTFMax && len(s) > 0 && !utf8.ValidString(s); i++ {
		s = s[:len(s)-1]
	}
	return s
}
