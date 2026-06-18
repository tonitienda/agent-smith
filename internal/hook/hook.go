// Package hook runs user-configured lifecycle hooks (AS-035, PRD §7.5):
// external commands fired at well-known points in a session — session
// start/stop, before and after a tool runs, before a compaction, and when the
// user submits a prompt — that can observe, block, modify, or annotate what the
// agent is about to do.
//
// A hook is a shell command named in config (AS-031). At each event the matching
// hooks run in order; each receives the event payload as JSON on stdin and
// decides the outcome through its exit code and an optional JSON object on
// stdout:
//
//   - exit 0, empty stdout            → allow (the default, do nothing);
//   - exit 0, {"decision":"block",…}  → block, feeding "reason" back to the model;
//   - exit 0, {"input":{…}}           → modify the tool's arguments (pre-tool-use);
//   - exit 0, {"annotation":"…"}      → append a note to the log;
//   - exit 2                          → block (reason from stdout JSON or stderr);
//   - any other non-zero / timeout    → hook *failure*: apply the hook's failure
//     policy (fail-open continues, fail-closed blocks) and surface a warning.
//
// Hooks are automation, not the security boundary: pre-tool-use hooks run after
// the permission check (AS-016), which remains the gate. A hanging or crashing
// hook never wedges the loop — every hook runs under a timeout, and a failure is
// always resolved to a defined outcome with a visible warning. The package is
// face- and runtime-agnostic and depends only on the stdlib: callers wire the
// returned Outcome into the tool runtime and the turn loop.
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"
)

// Event is a lifecycle point at which configured hooks fire. The six events are
// the PRD §7.5 set; their string values are what a hook Spec names in config and
// what the loop passes when firing.
type Event string

const (
	// SessionStart fires once when a session opens.
	SessionStart Event = "session-start"
	// SessionStop fires once when a session ends.
	SessionStop Event = "session-stop"
	// PreToolUse fires before a tool executes, after the permission check. It may
	// block the call or modify its arguments.
	PreToolUse Event = "pre-tool-use"
	// PostToolUse fires after a tool produces its result. It may annotate.
	PostToolUse Event = "post-tool-use"
	// PreCompact fires before a /compact (AS-038) reorganizes the context. It may
	// veto (block) or annotate.
	PreCompact Event = "pre-compact"
	// UserPromptSubmit fires when the user submits a prompt, before it is recorded.
	UserPromptSubmit Event = "user-prompt-submit"
)

// validEvents is the set of recognized event names, so a misspelled event in
// config surfaces as a warning rather than silently never firing.
var validEvents = map[Event]bool{
	SessionStart: true, SessionStop: true, PreToolUse: true,
	PostToolUse: true, PreCompact: true, UserPromptSubmit: true,
}

// DefaultTimeout bounds a single hook's run when its Spec sets none, so a hook
// that hangs cannot wedge the loop.
const DefaultTimeout = 30 * time.Second

// Spec is one hook as it appears in config (AS-031, the `hooks` array): the
// event it fires on, an optional matcher, the shell command to run, an optional
// timeout, and its failure policy. It is the JSON-decoded form; compile turns it
// into a runnable hook.
type Spec struct {
	// Event is the lifecycle point this hook fires on (one of the Event values).
	Event string `json:"event"`
	// Matcher is a glob (filepath.Match syntax) tested against the event's match
	// key — the tool name for tool events. Empty or "*" matches every key, so a
	// hook with no matcher fires on all of its event's occurrences.
	Matcher string `json:"matcher,omitempty"`
	// Command is the shell command run via `sh -c`. Required.
	Command string `json:"command"`
	// Timeout is a Go duration string (e.g. "5s"); empty uses DefaultTimeout.
	Timeout string `json:"timeout,omitempty"`
	// FailOpen decides what a hook *failure* (non-block non-zero exit, a timeout,
	// or a launch error) resolves to: true continues as if allowed, false blocks.
	// The default (false) is fail-closed, the safe choice for a gate.
	FailOpen bool `json:"failOpen,omitempty"`
}

// hook is a compiled, runnable Spec.
type hook struct {
	event    Event
	matcher  string
	command  string
	timeout  time.Duration
	failOpen bool
}

// Warning is a non-fatal problem loading a Spec — an unknown event, a missing
// command, an unparsable timeout — surfaced so a misconfiguration is visible
// rather than silently dropped (mirrors config's tolerate-but-warn ethos).
type Warning struct {
	Index   int
	Message string
}

func (w Warning) String() string { return fmt.Sprintf("hooks[%d]: %s", w.Index, w.Message) }

// Set is the compiled collection of hooks grouped by event. A nil *Set is valid
// and fires nothing, so a session with no configured hooks needs no special
// casing. It is immutable after construction and safe for concurrent use.
type Set struct {
	byEvent map[Event][]hook
}

// configDecoder is the slice of the config the loader reads its hooks from; the
// config package's *Config satisfies it via Decode. Kept as an interface so this
// package does not import config (layering: config consumers depend on config,
// not the reverse).
type configDecoder interface {
	Decode(path string, v any) (bool, error)
}

// Load reads the `hooks` array out of cfg and compiles it into a Set, returning
// any per-spec warnings. A missing or empty `hooks` key yields an empty Set and
// no error, so hooks are purely opt-in. A malformed `hooks` value (not an array)
// is the only hard error.
func Load(cfg configDecoder) (*Set, []Warning, error) {
	var specs []Spec
	ok, err := cfg.Decode("hooks", &specs)
	if err != nil {
		return nil, nil, fmt.Errorf("hook: load config: %w", err)
	}
	if !ok {
		return New(nil), nil, nil
	}
	return Compile(specs)
}

// Compile turns decoded specs into a Set, dropping (with a warning) any spec
// that is unusable: an unknown event, an empty command, or an unparsable
// timeout. The order of usable hooks within an event is preserved.
func Compile(specs []Spec) (*Set, []Warning, error) {
	byEvent := map[Event][]hook{}
	var warnings []Warning
	for i, s := range specs {
		ev := Event(strings.TrimSpace(s.Event))
		if !validEvents[ev] {
			warnings = append(warnings, Warning{i, fmt.Sprintf("unknown event %q; skipped", s.Event)})
			continue
		}
		if strings.TrimSpace(s.Command) == "" {
			warnings = append(warnings, Warning{i, "missing command; skipped"})
			continue
		}
		// A malformed glob would make path.Match error at run time and so silently
		// never match; reject it here instead, like a bad timeout or unknown event.
		if s.Matcher != "" && s.Matcher != "*" {
			if _, err := path.Match(s.Matcher, ""); err != nil {
				warnings = append(warnings, Warning{i, fmt.Sprintf("invalid matcher %q; skipped", s.Matcher)})
				continue
			}
		}
		to := DefaultTimeout
		if s.Timeout != "" {
			d, err := time.ParseDuration(s.Timeout)
			if err != nil || d <= 0 {
				warnings = append(warnings, Warning{i, fmt.Sprintf("invalid timeout %q; skipped", s.Timeout)})
				continue
			}
			to = d
		}
		byEvent[ev] = append(byEvent[ev], hook{
			event:    ev,
			matcher:  s.Matcher,
			command:  s.Command,
			timeout:  to,
			failOpen: s.FailOpen,
		})
	}
	return New(byEvent), warnings, nil
}

// New wraps an already-grouped map of hooks (used by Compile and tests).
func New(byEvent map[Event][]hook) *Set {
	if byEvent == nil {
		byEvent = map[Event][]hook{}
	}
	return &Set{byEvent: byEvent}
}

// Has reports whether the Set has any hook for event — a cheap guard so a caller
// can skip building a payload when nothing will fire.
func (s *Set) Has(event Event) bool {
	if s == nil {
		return false
	}
	return len(s.byEvent[event]) > 0
}

// Payload is the event data handed to a hook as JSON on stdin. Only the fields
// relevant to an event are set; the rest are omitted. Event is always present so
// a single hook script can switch on it.
type Payload struct {
	Event Event `json:"event"`
	// Session is the session id, set for every event so a hook can correlate.
	Session string `json:"session,omitempty"`
	// Tool is the tool name, set for pre/post-tool-use; it is also the match key.
	Tool string `json:"tool,omitempty"`
	// Input is the tool's arguments object (pre-tool-use) — what a modify hook
	// rewrites.
	Input json.RawMessage `json:"input,omitempty"`
	// Result is the tool's result content (post-tool-use), as recorded on the log.
	Result json.RawMessage `json:"result,omitempty"`
	// IsError reports whether the tool result was an error (post-tool-use).
	IsError bool `json:"isError,omitempty"`
	// Prompt is the user's submitted text (user-prompt-submit).
	Prompt string `json:"prompt,omitempty"`
	// Reason is why a compaction was triggered (pre-compact).
	Reason string `json:"reason,omitempty"`
}

// matchKey is the value a hook's matcher is tested against for this payload.
func (p Payload) matchKey() string { return p.Tool }

// response is the JSON a hook may print on stdout to steer the outcome. Every
// field is optional; an empty object (or no output) means "allow, unchanged".
type response struct {
	// Decision is "block" to stop the action, "allow" (or empty) to continue.
	Decision string `json:"decision,omitempty"`
	// Reason is the model-facing explanation when blocking, or the note text when
	// annotating.
	Reason string `json:"reason,omitempty"`
	// Input replaces the tool's arguments (pre-tool-use only). A nil value leaves
	// them unchanged.
	Input json.RawMessage `json:"input,omitempty"`
	// Annotation is a note appended to the log (any event).
	Annotation string `json:"annotation,omitempty"`
}

// Outcome is the combined result of running every matching hook for an event.
// The zero Outcome is "allow, unchanged, nothing to note" — what an event with
// no hooks (or only no-op hooks) produces.
type Outcome struct {
	// Blocked reports that a hook vetoed the action; Reason carries why, to feed
	// back to the model (block) or surface to the user (veto).
	Blocked bool
	Reason  string
	// Input is the rewritten tool arguments when a pre-tool-use hook modified
	// them; nil means unchanged. When several hooks modify in turn, this is the
	// last one's output (each hook sees the prior hook's modification).
	Input json.RawMessage
	// Annotations are notes hooks asked to append to the log, in fire order.
	Annotations []string
	// Warnings record hooks that failed (timeout, launch error, or an unexpected
	// non-zero exit) and how their failure policy resolved, so a face can show the
	// operator that a hook misbehaved.
	Warnings []string
}

// Run fires every hook registered for p.Event whose matcher matches p's match
// key, in order, and folds their responses into one Outcome. It never returns an
// error: a hook that fails (times out, cannot launch, or exits non-zero outside
// the block convention) is resolved through its failure policy and recorded as a
// warning, so the caller's loop is never wedged by a misbehaving hook.
//
// The first hook to block stops the chain and returns the block. Modifications
// chain: a hook that rewrites the tool input feeds the rewritten input to the
// next hook in p.Input, so later hooks see earlier edits.
func (s *Set) Run(ctx context.Context, p Payload) Outcome {
	var out Outcome
	if s == nil {
		return out
	}
	key := p.matchKey()
	for _, h := range s.byEvent[p.Event] {
		if !h.matches(key) {
			continue
		}
		resp, failure := h.run(ctx, p)
		if failure != "" {
			if h.failOpen {
				out.Warnings = append(out.Warnings, failure+" (fail-open: continued)")
				continue
			}
			out.Warnings = append(out.Warnings, failure+" (fail-closed: blocked)")
			out.Blocked = true
			out.Reason = failure
			return out
		}
		if resp.Annotation != "" {
			out.Annotations = append(out.Annotations, resp.Annotation)
		}
		if strings.EqualFold(resp.Decision, "block") {
			out.Blocked = true
			out.Reason = resp.Reason
			return out
		}
		if resp.Input != nil {
			out.Input = resp.Input
			p.Input = resp.Input // chain the modification into later hooks
		}
	}
	return out
}

// matches reports whether the hook's matcher applies to key. An empty or "*"
// matcher matches everything; otherwise filepath.Match glob syntax is used, and
// a malformed pattern matches nothing (it was already a no-op).
func (h hook) matches(key string) bool {
	if h.matcher == "" || h.matcher == "*" {
		return true
	}
	ok, err := path.Match(h.matcher, key)
	return err == nil && ok
}

// run executes one hook with p as JSON on stdin under the hook's timeout. It
// returns the parsed response on a clean run, or a non-empty failure string
// describing why the hook failed (to be resolved by the caller's failure policy).
// The block convention (exit 2, or an explicit {"decision":"block"}) is a clean
// run, not a failure: it returns a response, never a failure string.
func (h hook) run(ctx context.Context, p Payload) (response, string) {
	stdin, err := json.Marshal(p)
	if err != nil {
		return response{}, fmt.Sprintf("hook %q: encode payload: %v", h.command, err)
	}

	runCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", h.command) //nolint:gosec // hook commands are operator-configured by design (AS-035)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if runCtx.Err() == context.DeadlineExceeded {
		return response{}, fmt.Sprintf("hook %q: timed out after %s", h.command, h.timeout)
	}

	if runErr == nil {
		resp, perr := parseResponse(stdout.Bytes())
		if perr != nil {
			return response{}, fmt.Sprintf("hook %q: malformed stdout: %v", h.command, perr)
		}
		return resp, ""
	}

	// Exit 2 is the block convention: a clean veto, not a failure.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 2 {
		return blockResponse(stdout.Bytes(), stderr.String()), ""
	}

	// Any other non-zero exit (or a launch error) is a hook failure.
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = runErr.Error()
	}
	return response{}, fmt.Sprintf("hook %q: %s", h.command, detail)
}

// parseResponse decodes a hook's stdout into a response. Empty (or whitespace)
// output is a valid no-op response; non-empty output must be a JSON object.
func parseResponse(out []byte) (response, error) {
	if len(bytes.TrimSpace(out)) == 0 {
		return response{}, nil
	}
	var r response
	if err := json.Unmarshal(out, &r); err != nil {
		return response{}, err
	}
	return r, nil
}

// blockResponse builds the response for an exit-2 veto: a block whose reason
// comes from a JSON {"reason":…} on stdout if present, else the raw stderr text,
// else a generic message.
func blockResponse(stdout []byte, stderr string) response {
	if r, err := parseResponse(stdout); err == nil && r.Reason != "" {
		return response{Decision: "block", Reason: r.Reason}
	}
	reason := strings.TrimSpace(stderr)
	if reason == "" {
		reason = "blocked by hook"
	}
	return response{Decision: "block", Reason: reason}
}
