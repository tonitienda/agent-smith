package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/tonitienda/agent-smith/internal/permission"
	"github.com/tonitienda/agent-smith/internal/tui"
)

// buildPolicy loads the layered (user then project) permission config and builds
// the approval policy for the session, routing prompts to the TUI and persisting
// "always allow" rules to the project config (AS-016/AS-024). With no config file
// the default is ModeAsk, so every tool call prompts — the conservative posture
// of PRD D9 ("you approve actions"); a project can relax it with a
// .smith/permissions.json (allowlist/auto, per-tool overrides).
func buildPolicy(root string, app *tui.App) (*permission.Policy, error) {
	cfg, err := permission.LoadLayered(permission.UserConfigPath(), permission.ProjectConfigPath(root))
	if err != nil {
		return nil, err
	}
	return permission.New(cfg,
		permission.WithAsker(tuiAsker{app: app}),
		permission.WithPersister(permission.FilePersister(permission.ProjectConfigPath(root))),
	), nil
}

// tuiAsker adapts the TUI's approval surface (App.Ask) to the permission.Asker
// the policy calls before a gated tool runs. It lives here, in the command, so
// internal/tui never imports internal/permission (or internal/tool) — the face
// stays decoupled behind the face-agnostic PermissionPrompt seam.
type tuiAsker struct{ app *tui.App }

// Ask renders the request and returns the user's decision. It is called on the
// turn goroutine and may be called concurrently for parallel tool calls (AS-019);
// App.Ask serializes them into the Update loop.
func (a tuiAsker) Ask(ctx context.Context, req permission.Request) (permission.Outcome, error) {
	prompt := tui.PermissionPrompt{
		Tool:        req.Tool,
		Subject:     req.Subject,
		Detail:      editDiff(req),
		Destructive: destructive(req.Tool),
	}
	d, err := a.app.Ask(ctx, prompt)
	if err != nil {
		return permission.Outcome{}, err
	}
	return permission.Outcome{Allow: d.Allow, Remember: d.Remember}, nil
}

// maxDiffLines caps how many lines an edit diff renders in a permission prompt;
// beyond it the diff is truncated with a marker, since the inline card shows only
// a handful of rows anyway.
const maxDiffLines = 40

// destructive flags the broad-scope tools that escalate to a focus-trapped
// blocking modal rather than an inline card (D-TUI-8). The shell tool runs an
// arbitrary command with the user's privileges — the broadest scope — so it traps
// focus; file reads/writes/edits use the inline card.
func destructive(toolName string) bool { return toolName == "shell" }

// editDiff renders a unified-style diff of an edit call's replacement so the user
// reviews the exact change before approving it (AS-024 AC2). It is a block diff —
// the removed text then the added text — which is the honest, minimal description
// of an edit's old_string → new_string swap. Non-edit calls (and unparseable
// arguments) have no diff.
func editDiff(req permission.Request) string {
	if req.Tool != "edit" {
		return ""
	}
	var args struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return ""
	}
	var lines []string
	// Trim a single trailing newline (standard for POSIX file content) before
	// splitting, so the diff doesn't end in a spurious empty "- "/"+ " line.
	if args.OldString != "" {
		for _, ln := range strings.Split(strings.TrimSuffix(args.OldString, "\n"), "\n") {
			lines = append(lines, "- "+ln)
		}
	}
	if args.NewString != "" {
		for _, ln := range strings.Split(strings.TrimSuffix(args.NewString, "\n"), "\n") {
			lines = append(lines, "+ "+ln)
		}
	}
	// A large multi-hundred-line edit would build (and the TUI would style)
	// far more than the inline card can show, so cap the rendered diff.
	if len(lines) > maxDiffLines {
		lines = append(lines[:maxDiffLines], "… (diff truncated)")
	}
	return strings.Join(lines, "\n")
}
