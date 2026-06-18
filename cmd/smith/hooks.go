package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/hook"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// hookProducer attributes hook-note events on the log.
const hookProducer = "hook"

// loadHooks builds the lifecycle-hook set (AS-035) from the layered config's
// `hooks` array, printing any per-spec warnings (an unknown event, a missing
// command) without failing the session — a misconfigured hook is skipped, never
// fatal, mirroring config's tolerate-but-warn ethos. A nil *Set fires nothing, so
// the caller never needs to special-case "no hooks configured".
func loadHooks(cfg *config.Config, stderr io.Writer) *hook.Set {
	set, warns, err := hook.Load(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: hooks: %v\n", err)
		return hook.New(nil)
	}
	for _, w := range warns {
		_, _ = fmt.Fprintf(stderr, "warning: config: %s\n", w)
	}
	return set
}

// hookToolOptions bridges the hook set to the tool runtime's pre/post-tool-use
// seams (AS-035), recording any hook annotations and warnings on log. It returns
// no options when no tool hooks are configured, so a hook-free session builds the
// runtime exactly as before.
func hookToolOptions(hooks *hook.Set, log *eventlog.Log, sessionID string) []tool.Option {
	var opts []tool.Option
	if hooks.Has(hook.PreToolUse) {
		opts = append(opts, tool.WithPreToolHook(func(ctx context.Context, c tool.Call) tool.PreToolResult {
			out := hooks.Run(ctx, hook.Payload{
				Event:   hook.PreToolUse,
				Session: sessionID,
				Tool:    c.Name,
				Input:   c.Arguments,
			})
			recordHookOutcome(log, hook.PreToolUse, out)
			return tool.PreToolResult{Block: out.Blocked, Reason: out.Reason, Modified: out.Input}
		}))
	}
	if hooks.Has(hook.PostToolUse) {
		opts = append(opts, tool.WithPostToolHook(func(ctx context.Context, c tool.Call, r *schema.ToolResultBody) {
			out := hooks.Run(ctx, hook.Payload{
				Event:   hook.PostToolUse,
				Session: sessionID,
				Tool:    c.Name,
				Result:  resultJSON(r),
				IsError: r.IsError,
			})
			recordHookOutcome(log, hook.PostToolUse, out)
		}))
	}
	return opts
}

// fireLifecycle runs a lifecycle event's hooks (session start/stop, prompt
// submit), records their annotations and warnings on log, and returns the
// outcome so the caller can honor a block or a modification. A nil set is inert.
func fireLifecycle(ctx context.Context, hooks *hook.Set, log *eventlog.Log, p hook.Payload) hook.Outcome {
	out := hooks.Run(ctx, p)
	recordHookOutcome(log, p.Event, out)
	return out
}

// recordHookOutcome appends a hook's annotations and any failure warnings to the
// log as hook-note control events (AS-035), so the operator can audit what a hook
// did without those notes entering model-facing context. Append errors are
// ignored: a failed note must never wedge the turn.
func recordHookOutcome(log *eventlog.Log, ev hook.Event, out hook.Outcome) {
	if log == nil {
		return
	}
	for _, note := range out.Annotations {
		_, _ = log.Append(eventlog.NewHookNote(hookProducer, string(ev), note))
	}
	for _, w := range out.Warnings {
		_, _ = log.Append(eventlog.NewHookNote(hookProducer, string(ev), "warning: "+w))
	}
}

// resultJSON renders a tool result's content as a compact JSON value for a
// post-tool-use hook's payload, best-effort: an encode failure yields nil rather
// than failing the hook.
func resultJSON(r *schema.ToolResultBody) json.RawMessage {
	if r == nil {
		return nil
	}
	b, err := json.Marshal(r.Content)
	if err != nil {
		return nil
	}
	return b
}
