// Package cli is Agent Smith's command-line router (AS-065): it parses argv into
// a subcommand and arguments, applies the shared global flags, dispatches to a
// handler, and maps the handler's result to the D-CLI-7 exit codes
// (0 success / 1 runtime failure / 2 invalid usage).
//
// It is the face-neutral *spine* the CLI-UX.md decisions describe (D-CLI-1..10):
// a git/kubectl-style verb tree (subcommand-first, noun-grouped) whose handlers
// resolve through internal/command — the same registry the TUI palette renders —
// so a slash-command and its CLI subcommand dispatch to one handler. The package
// knows nothing about the provider/tool/session layers or the TUI: cmd/smith
// wires the real handlers and the bare-invocation TUI launch. The headless
// feature set (output streaming, budgets, permission posture) lives on `run`
// (AS-051, cmd/smith/headless.go); full slash<->subcommand parity in AS-066.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tonitienda/agent-smith/internal/command"
)

// Exit codes — the universal three V1 commits to (D-CLI-7) plus the headless
// taxonomy AS-051 assigns additively on top (UX.md §17.2 / D-CLI-7). Codes are
// append-only: a script may treat any unknown nonzero code as a generic failure,
// so widening the set never breaks an existing consumer.
const (
	ExitOK    = 0 // success
	ExitFail  = 1 // runtime/task failure (the generic internal-error bucket)
	ExitUsage = 2 // invalid usage (bad flag/arg/command)

	// The headless classes (AS-051): a `smith run` outcome that is neither plain
	// success nor a usage error carries one of these so a script can branch on
	// *why* it stopped. A handler signals one by returning an *ExitError.
	ExitPermission = 3 // a tool call was denied by the headless permission posture (D-CLI-9)
	ExitBudget     = 4 // the run stopped at the budget ceiling (AS-041)
	ExitCanceled   = 5 // the run was canceled (context cancellation / interrupt)
	ExitProvider   = 6 // a provider error ended the run (auth, rate limit, overloaded, …)
)

// ExitError lets a handler choose its process exit code beyond the usage/runtime
// split: App.Run maps it to Code. Err, when non-nil, is printed as the stderr
// diagnostic the way any other handler error is; a nil Err exits with Code
// silently (the handler already wrote a structured result to stdout). It is the
// seam the headless taxonomy (AS-051) uses to surface permission/budget/provider
// stops as distinct codes.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}
func (e *ExitError) Unwrap() error { return e.Err }

// UsageError marks an invalid-usage failure (bad flag, missing/unknown
// subcommand): App.Run renders it to stderr and exits ExitUsage. Any other error
// a handler returns is a runtime failure (ExitFail).
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// Usagef builds a UsageError from a format string.
func Usagef(format string, a ...any) error { return &UsageError{Err: fmt.Errorf(format, a...)} }

// errUsageSilent is returned by helpers that have already written their own
// diagnostic (an unknown-command suggestion, a group with no verb): App.Run
// exits ExitUsage without printing it again.
var errUsageSilent = &UsageError{Err: errors.New("usage")}

// Command is one node in the verb tree. A leaf sets Run; a noun group sets Sub
// (e.g. `session list|resume`) and leaves Run nil.
type Command struct {
	// Name is the invocation token (e.g. "run", "list").
	Name string
	// Summary is the one-line description for help and the parity table.
	Summary string
	// Usage is the argument spec for help, e.g. "<prompt>" or "<key> [value]".
	Usage string
	// Examples are runnable invocations shown under the command's help (D-CLI-10).
	Examples []string
	// Scriptability is the command's parity classification (UX.md §17.5):
	// "interactive-only" | "scriptable" | "both". Shared verbs copy it from the
	// command.Registry descriptor (cmd/smith) so the two faces can't disagree; it
	// surfaces in `--help --output json`. Empty is treated as unstated.
	Scriptability string
	// Reason explains an interactive-only command (UX.md §17.5); shown in JSON help.
	Reason string
	// OutputSchema names the per-command structured output beyond the shared
	// `{text}` envelope, where one exists; empty otherwise (additive, AS-051).
	OutputSchema string
	// Flags registers command-specific flags (e.g. -f on `run`) onto the parse
	// set; the global flags are always registered too. Optional.
	Flags func(*flag.FlagSet)
	// Run executes a leaf command. A non-nil Sub makes this a group and Run is
	// ignored.
	Run func(*Context) error
	// Sub holds the verbs of a noun group.
	Sub []*Command
}

// Context is what a handler receives: the post-flag positional arguments, the
// resolved global options, and the IO streams. Data goes to Stdout, diagnostics
// to Stderr (D-CLI-5).
type Context struct {
	Args      []string
	Globals   Globals
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	StdinTTY  bool // stdin is a terminal (no piped prompt; D-CLI-3)
	StdoutTTY bool // stdout is a terminal (interactive; destructive --yes, D-CLI-8)
}

// App is the configured router. cmd/smith builds one per process and calls Run.
type App struct {
	Name      string
	Tagline   string
	Version   string
	Commands  []*Command
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	StdinTTY  bool
	StdoutTTY bool
	// Getenv resolves environment variables (NO_COLOR, SMITH_*); nil reads nothing.
	Getenv func(string) string
	// Bare runs on `smith` with no args and an interactive terminal — the
	// zero-friction TUI launch (D-CLI-2). nil disables it (off a TTY it is never
	// reached: no-args + non-TTY prints usage and exits ExitUsage).
	Bare func(*Context) error
}

// Run routes args and returns the process exit code. It never panics on bad
// input: malformed usage yields ExitUsage with a diagnostic on stderr.
func (a *App) Run(args []string) int {
	if a.Getenv == nil {
		a.Getenv = func(string) string { return "" }
	}

	// Bare invocation (D-CLI-2): no args + TTY launches the TUI; no args + non-TTY
	// is a usage error so the binary stays well-behaved in pipes/CI.
	if len(args) == 0 {
		if a.StdinTTY && a.StdoutTTY && a.Bare != nil {
			return a.exit(a.Bare(a.context(nil, Globals{})))
		}
		write(a.Stderr, a.rootHelp())
		return ExitUsage
	}

	switch head := args[0]; {
	case head == "--version" || head == "-V":
		write(a.Stdout, a.Version+"\n")
		return ExitOK
	case head == "--help" || head == "-h":
		write(a.Stdout, a.rootHelp())
		return ExitOK
	case strings.HasPrefix(head, "-"):
		write(a.Stderr, fmt.Sprintf("%s: unknown flag %q\n\n%s", a.Name, head, a.rootHelp()))
		return ExitUsage
	default:
		cmd := find(a.Commands, head)
		if cmd == nil {
			a.unknown(a.Commands, head, "")
			return ExitUsage
		}
		return a.exit(a.dispatch(cmd, args[1:], head))
	}
}

// dispatch routes into a noun group or runs a leaf command. path is the
// space-joined command chain so far ("session resume"), for help and errors.
func (a *App) dispatch(cmd *Command, args []string, path string) error {
	if len(cmd.Sub) > 0 {
		return a.dispatchGroup(cmd, args, path)
	}
	return a.runLeaf(cmd, args, path)
}

// dispatchGroup handles a noun group: pick the verb, suggest on a typo, show the
// group's help on `--help` or no verb.
func (a *App) dispatchGroup(cmd *Command, args []string, path string) error {
	if len(args) == 0 {
		// A group with no verb is a usage error; print its help to stderr.
		write(a.Stderr, a.groupHelp(cmd, path))
		return errUsageSilent
	}
	if args[0] == "--help" || args[0] == "-h" {
		write(a.Stdout, a.groupHelp(cmd, path))
		return nil
	}
	sub := find(cmd.Sub, args[0])
	if sub == nil {
		a.unknown(cmd.Sub, args[0], path+" ")
		return errUsageSilent
	}
	return a.dispatch(sub, args[1:], path+" "+args[0])
}

// runLeaf parses the leaf's flags (globals + command-specific), resolves the
// output/color options, and invokes the handler. `--help` renders this command's
// help (text, or the registry entry as JSON under `--output json`, D-CLI-10).
func (a *App) runLeaf(cmd *Command, args []string, path string) error {
	fs := flag.NewFlagSet(path, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	gv := registerGlobals(fs)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	// stdlib flag stops at the first positional, but D-CLI-4 needs flags to work
	// after the prompt (`smith run "…" --output json`), so permute flags ahead of
	// positionals first.
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return Usagef("%s: %v", path, err)
	}

	globals, err := a.resolveGlobals(gv)
	if err != nil {
		return Usagef("%s: %v", path, err)
	}
	if *gv.help {
		if globals.Output == OutputJSON || globals.Output == OutputStreamJSON {
			return a.writeCommandHelpJSON(cmd, path)
		}
		write(a.Stdout, a.commandHelp(cmd, path))
		return nil
	}

	return cmd.Run(a.context(fs.Args(), globals))
}

// context builds a handler Context from the app's streams.
func (a *App) context(args []string, g Globals) *Context {
	return &Context{
		Args:      args,
		Globals:   g,
		Stdin:     a.Stdin,
		Stdout:    a.Stdout,
		Stderr:    a.Stderr,
		StdinTTY:  a.StdinTTY,
		StdoutTTY: a.StdoutTTY,
	}
}

// exit maps a handler error to an exit code, printing the diagnostic to stderr.
// A UsageError exits ExitUsage; anything else is a runtime failure (ExitFail).
func (a *App) exit(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errUsageSilent):
		return ExitUsage // helper already printed its own diagnostic
	}
	// A handler that chose its own exit code (the headless taxonomy, AS-051) wins:
	// print its diagnostic only when it carries one, since it has typically already
	// written a structured result to stdout.
	var ee *ExitError
	if errors.As(err, &ee) {
		if ee.Err != nil {
			write(a.Stderr, fmt.Sprintf("%s: %v\n", a.Name, ee.Err))
		}
		return ee.Code
	}
	write(a.Stderr, fmt.Sprintf("%s: %v\n", a.Name, err))
	var ue *UsageError
	if errors.As(err, &ue) {
		return ExitUsage
	}
	return ExitFail
}

// unknown reports an unknown command with a "did you mean …?" suggestion, drawn
// from the same scoring the slash palette uses (command.Nearest). prefix carries
// the group path so a bad verb suggests "session resume", not "resume".
func (a *App) unknown(cmds []*Command, name, prefix string) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: unknown command %q\n", a.Name, prefix+name)
	if s, ok := command.Nearest(name, namesOf(cmds)); ok {
		fmt.Fprintf(&b, "Did you mean %q?\n", strings.TrimSpace(prefix)+pick(prefix, s))
	}
	fmt.Fprintf(&b, "Run '%s --help' for usage.\n", a.Name)
	write(a.Stderr, b.String())
}

// write sends s to w, discarding the result: a failed write to a terminal's
// stdout/stderr (a closed pipe) is not separately actionable in a diagnostic
// path. Command *data* output goes through Context.Emit, which does surface
// write errors.
func write(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}

// pick formats the suggestion under a group prefix as " resume" → "session resume".
func pick(prefix, s string) string {
	if prefix == "" {
		return s
	}
	return " " + s
}

// reorder permutes args so every flag precedes the positionals, letting the
// stdlib flag parser (which stops at the first non-flag) accept flags written
// after positionals — `smith run "prompt" --output json`. A bare "--" ends flag
// parsing: everything after it is positional. A flag that takes a value
// (anything but a bool) carries its following token along when not written as
// `--flag=value`.
func reorder(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return append(flags, positional...)
		case len(a) > 1 && a[0] == '-':
			flags = append(flags, a)
			// `--name=value` is self-contained; a non-bool `--name value` needs the
			// next token too.
			if !strings.Contains(a, "=") && takesValue(fs, a) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

// takesValue reports whether the flag token names a registered non-bool flag (so
// the next token is its value). Unknown flags are treated as value-less; flag's
// own parser then reports the error.
func takesValue(fs *flag.FlagSet, token string) bool {
	name := strings.TrimLeft(token, "-")
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !ok || !bf.IsBoolFlag()
}

// find returns the command named name, or nil.
func find(cmds []*Command, name string) *Command {
	for _, c := range cmds {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// namesOf lists command names for suggestion scoring.
func namesOf(cmds []*Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
