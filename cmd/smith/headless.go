package main

import (
	"context"
	"errors"
	"sync"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/permission"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/tool"
)

// runResult is the structured outcome of a headless `smith run`, emitted under
// `--output json` (and as the terminal object of `--output stream-json`). The
// fields are the ones D-CLI-4 / CLI-UX.md §2 names — the answer, what it cost,
// which session captured it (so it is `/resume`-able, AS-051 AC4), and why the
// run stopped — plus the structured permission report (D-CLI-9) so a script can
// see, machine-readably, which tool the headless posture denied. New fields are
// additive (PRD D2): a consumer ignores keys it does not know.
type runResult struct {
	Text       string       `json:"text"`
	SessionID  string       `json:"session_id"`
	StopReason string       `json:"stop_reason"`
	CostUSD    float64      `json:"cost_usd"`
	Iterations int          `json:"iterations"`
	Denied     []deniedCall `json:"denied,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// deniedCall records one tool call the headless permission posture refused, with
// the model-readable reason the gate returned (D-CLI-9: "denied with a structured
// report, never a hang").
type deniedCall struct {
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

// streamEvent is one line of `--output stream-json`: a face-agnostic loop UIEvent
// flattened to the fields a headless consumer can use. It is the same event
// substrate the TUI renders (UX.md §17.4) — one stream, many renderers — tagged
// with `type` so a reader can switch on it and skip kinds it does not recognize.
type streamEvent struct {
	Type       string  `json:"type"`
	Iteration  int     `json:"iteration"`
	Text       string  `json:"text,omitempty"`
	Tool       string  `json:"tool,omitempty"`
	StopReason string  `json:"stop_reason,omitempty"`
	SpentUSD   float64 `json:"spent_usd,omitempty"`
	LimitUSD   float64 `json:"limit_usd,omitempty"`
}

// streamObserver returns a loop.Observer that writes each UIEvent as one JSON line
// to the context's stdout, for `--output stream-json`. For any other output mode
// it returns nil so the engine streams nothing extra.
func streamObserver(c *cli.Context) loop.Observer {
	if c.Globals.Output != cli.OutputStreamJSON {
		return nil
	}
	// Parallel tool execution (AS-019) invokes the observer from several goroutines
	// at once, so serialize the stdout writes — otherwise two JSON lines could
	// interleave into one corrupt line.
	var mu sync.Mutex
	return func(ev loop.UIEvent) {
		se := streamEvent{
			Type:       string(ev.Kind),
			Iteration:  ev.Iteration,
			Text:       ev.Text,
			StopReason: ev.StopReason,
			SpentUSD:   ev.BudgetSpentUSD,
			LimitUSD:   ev.BudgetLimitUSD,
		}
		if ev.Tool != nil {
			se.Tool = ev.Tool.Name
		}
		mu.Lock()
		_ = c.WriteJSON(se)
		mu.Unlock()
	}
}

// denialRecorder wraps a permission decision function to capture every call the
// posture denied, so the headless result can report them (D-CLI-9) and the run
// can exit with the permission-stop code. It is safe for concurrent use because
// parallel tool calls (AS-019) may pass the gate at once.
type denialRecorder struct {
	inner tool.PermissionFunc
	mu    sync.Mutex
	calls []deniedCall
}

func (d *denialRecorder) decide(ctx context.Context, call tool.Call) tool.Decision {
	dec := d.inner(ctx, call)
	if !dec.Allow {
		d.mu.Lock()
		d.calls = append(d.calls, deniedCall{Tool: call.Name, Reason: dec.Reason})
		d.mu.Unlock()
	}
	return dec
}

func (d *denialRecorder) denied() []deniedCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) == 0 {
		return nil
	}
	return append([]deniedCall(nil), d.calls...)
}

// headlessPermission builds the permission gate for a headless run (D-CLI-9). The
// default posture is allowlist-then-deny: the project/user permission config
// decides, but with no interactive Asker every call that would prompt resolves to
// a denial rather than a hang. `--auto` opts explicitly into auto mode — every
// call runs unattended (Async Ana). Either way the gate is wrapped so denials are
// recorded for the result report.
func headlessPermission(root, override string, auto bool) (*denialRecorder, error) {
	var policy *permission.Policy
	if auto {
		policy = permission.New(permission.Config{DefaultMode: permission.ModeAuto})
	} else {
		p, err := buildHeadlessPolicy(root, override)
		if err != nil {
			return nil, err
		}
		policy = p
	}
	return &denialRecorder{inner: policy.Func()}, nil
}

// buildHeadlessPolicy loads the same merged permission config the TUI uses
// (config `permissions` section + legacy permissions.json), but wires no Asker
// and no Persister: a non-matching call has nowhere to prompt, so it is denied —
// the allowlist-then-deny posture (D-CLI-9). A project that wants a headless run
// to use a tool grants it an allow-rule (or runs with `--auto`).
func buildHeadlessPolicy(root, override string) (*permission.Policy, error) {
	cfg, err := mergedPermissionConfig(root, override)
	if err != nil {
		return nil, err
	}
	return permission.New(cfg), nil
}

// mergedPermissionConfig resolves the same merged permission configuration the
// TUI and headless faces use: the unified config `permissions` section overlaid
// with the legacy user/project permissions.json. The serve face (AS-077) reuses
// it, wiring an Asker on top so an ask-mode call is forwarded to the connected
// client instead of being denied outright.
func mergedPermissionConfig(root, override string) (permission.Config, error) {
	cfg, err := loadLayeredConfig(override)
	if err != nil {
		return permission.Config{}, err
	}
	unified, err := permission.ConfigFrom(cfg)
	if err != nil {
		return permission.Config{}, err
	}
	legacy, err := permission.LoadLayered(permission.UserConfigPath(), permission.ProjectConfigPath(root))
	if err != nil {
		return permission.Config{}, err
	}
	return permission.Merge(unified, legacy), nil
}

// classifyExit maps a finished headless run to its process exit code and the
// machine-readable reason for it (AS-051, the additive D-CLI-7 taxonomy). The
// order is significant: a returned error (cancellation, provider, internal)
// outranks a clean stop reason, and a budget stop outranks a permission denial,
// so the most decisive cause wins the code. A plain success with no denial is
// ExitOK.
func classifyExit(res loop.Result, runErr error, denied []deniedCall) (code int, reason string) {
	switch {
	case runErr == nil:
		// fall through to the stop-reason checks below.
	case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded):
		return cli.ExitCanceled, "canceled"
	default:
		if _, ok := provider.AsError(runErr); ok {
			return cli.ExitProvider, runErr.Error()
		}
		return cli.ExitFail, runErr.Error()
	}
	switch {
	case res.StopReason == loop.StopBudget:
		return cli.ExitBudget, "budget ceiling reached"
	case len(denied) > 0:
		return cli.ExitPermission, "tool permission denied in headless mode"
	default:
		return cli.ExitOK, ""
	}
}

// emitResult renders a headless run's outcome on stdout honoring --output
// (D-CLI-4): plain prints just the assistant's final text (clean, no personality
// — §7.21); json and stream-json print the structured runResult. For stream-json
// the per-event lines were already written by the stream observer, so this is the
// terminal result object. code/reason classify the outcome; a non-OK code is
// returned as a *cli.ExitError so the router exits with the right class while the
// result (already on stdout) carries the detail — the stderr diagnostic is left
// nil to avoid duplicating what the JSON already reports.
func emitResult(c *cli.Context, sessionID string, res loop.Result, costUSD float64, denied []deniedCall, runErr error) error {
	code, reason := classifyExit(res, runErr, denied)
	switch c.Globals.Output {
	case cli.OutputJSON, cli.OutputStreamJSON:
		out := runResult{
			Text:       res.FinalText,
			SessionID:  sessionID,
			StopReason: res.StopReason,
			CostUSD:    costUSD,
			Iterations: res.Iterations,
			Denied:     denied,
		}
		if code != cli.ExitOK {
			out.Error = reason
		}
		if err := c.WriteJSON(out); err != nil {
			return err
		}
	default:
		if err := c.Emit(res.FinalText); err != nil {
			return err
		}
	}
	if code == cli.ExitOK {
		return nil
	}
	// Plain mode has no structured channel for the reason, so surface it on stderr
	// there; JSON already carries it, so stay silent to keep stdout the sole record.
	var diag error
	if c.Globals.Output == cli.OutputPlain && reason != "" {
		diag = errors.New(reason)
	}
	return &cli.ExitError{Code: code, Err: diag}
}
