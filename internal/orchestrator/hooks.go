package orchestrator

// Deterministic GitHub hooks (AS-147). This is the reactive hook layer the ADR's
// D-ORCH-4 boundary places in internal/orchestrator: the narrow, non-cognitive
// label/comment/status side effects a job declares as explicit workflow steps and
// runs at lifecycle points (hooks) — never as prose an agent emits (ADR D-ORCH-6).
//
// The layer is deliberately split from its transport: the authenticated GitHub
// client that actually calls the API is AS-148 (scoped token in a proxy outside
// the runner, push restricted to the run's own branch), and the richer PR
// lifecycle actions (branch/PR/merge) are AS-149. This file owns only the mapping
// from a declared github.* hook step to a call on the [GitHubActions] port and the
// append-only session record of its outcome — all offline-testable with a fake
// port and no live credentials.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
)

// GitHubTarget names the issue or PR a deterministic hook acts on. Number is 0 for
// a run with no GitHub target (a cron or manual trigger), in which case the hook
// runner performs no action.
type GitHubTarget struct {
	Repository string // "owner/name"
	Number     int    // issue or PR number
}

// StatusUpdate is the payload for a github.set_status hook: a commit-status /
// check update on the target.
type StatusUpdate struct {
	State       string // e.g. success, failure, pending
	Context     string // status context label
	Description string
}

// GitHubActions is the deterministic side-effect port AS-147 hooks call. It is the
// narrow set of label/comment/status actions the reactive hook layer owns; the PR
// lifecycle actions (branch/PR/merge) are AS-149 and the authenticated transport
// that implements this interface against the GitHub API is AS-148. Each method
// returns a URL to the affected resource (when GitHub provides one) for the
// session record, and an error the hook runner records as a failed action before
// returning it.
type GitHubActions interface {
	AddLabel(ctx context.Context, t GitHubTarget, label string) (url string, err error)
	RemoveLabel(ctx context.Context, t GitHubTarget, label string) (url string, err error)
	Comment(ctx context.Context, t GitHubTarget, body string) (url string, err error)
	SetStatus(ctx context.Context, t GitHubTarget, s StatusUpdate) (url string, err error)
}

// TriggerContext is the JSON blob the daemon stamps onto a GitHub-triggered run
// (store.NewRun.TriggerContext) so a deterministic hook can target the originating
// issue/PR long after the webhook was normalised and the run was later claimed. It
// is exactly the subset of a [GitHubEvent] a hook needs; cron/manual runs carry an
// empty context.
type TriggerContext struct {
	Repository string   `json:"repository,omitempty"`
	Number     int      `json:"number,omitempty"`
	Actor      string   `json:"actor,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Command    string   `json:"command,omitempty"`
	Label      string   `json:"label,omitempty"`
	Base       string   `json:"base,omitempty"`
}

// triggerContextJSON marshals the event's targetable context for persistence on
// the enqueued run. The shape is fixed, so marshalling never fails.
func (ev GitHubEvent) triggerContextJSON() string {
	tc := TriggerContext{
		Repository: ev.Repository,
		Number:     ev.Number,
		Actor:      ev.Actor,
		Labels:     ev.Labels,
		Command:    ev.Command,
		Label:      ev.Label,
		Base:       ev.Base,
	}
	raw, _ := json.Marshal(tc) //nolint:errcheck // fixed-shape struct never fails to marshal
	return string(raw)
}

// decodeTriggerContext parses a run's stored trigger context. An empty or
// unparseable blob yields the zero context (no GitHub target), which the hook
// runner treats as "nothing to act on" rather than an error — a run must stay
// inspectable even if its context was never stamped.
func decodeTriggerContext(s string) TriggerContext {
	var tc TriggerContext
	if s == "" {
		return tc
	}
	_ = json.Unmarshal([]byte(s), &tc) //nolint:errcheck // best-effort; zero value is a valid "no target"
	return tc
}

// hookRecorder is the subset of *Recorder the hook runner writes through, kept as
// an interface so the runner is unit-testable without a session store.
type hookRecorder interface {
	GitHubAction(a GitHubAction) error
}

// runHooks executes the deterministic github.* steps a spec declares at one
// lifecycle point against the run's GitHub target, recording each action (success
// or failure) on the run's session. It is a no-op when no actions port is wired,
// the point declares no steps, or the run has no GitHub target (cron/manual runs)
// — so an orchestrator without GitHub credentials still runs cleanly.
//
// A step carrying a `when` guard is skipped fail-closed: the guard namespace
// (policy.*/trigger.*/steps.*) is owned by the policy engine (AS-157/AS-152), and
// performing a labelled/commented side effect on an unevaluated guard would be
// unsafe. The first action error stops the point and is returned, so the caller
// decides whether it fails the run (on_start) or is merely logged (terminal hooks).
func runHooks(ctx context.Context, actions GitHubActions, rec hookRecorder, steps []spec.Step, tc TriggerContext) error {
	// A run with no repository or no positive issue/PR number (cron/manual, or a
	// GitHub event that named no target) has nothing to act on; every AS-147 action
	// targets an issue/PR, so acting with Number 0 would only produce API errors.
	if actions == nil || len(steps) == 0 || tc.Repository == "" || tc.Number <= 0 {
		return nil
	}
	target := GitHubTarget{Repository: tc.Repository, Number: tc.Number}
	for _, st := range steps {
		if st.When != "" {
			continue
		}
		if err := runHookStep(ctx, actions, rec, st, target, tc); err != nil {
			return err
		}
	}
	return nil
}

// runHookStep dispatches one deterministic hook step to the actions port and
// records the outcome. Steps naming a github.* action AS-147 does not own (the PR
// lifecycle actions AS-149 owns) are ignored here so their own step executor can
// handle them.
func runHookStep(ctx context.Context, actions GitHubActions, rec hookRecorder, st spec.Step, target GitHubTarget, tc TriggerContext) error {
	act := GitHubAction{Repository: target.Repository, PRNumber: target.Number}
	var url string
	var err error
	switch st.Uses {
	case "github.add_label":
		label := withString(st, "label")
		act.Action, act.Ref = "add_label", label
		url, err = actions.AddLabel(ctx, target, label)
	case "github.remove_label":
		label := withString(st, "label")
		act.Action, act.Ref = "remove_label", label
		url, err = actions.RemoveLabel(ctx, target, label)
	case "github.comment":
		act.Action = "comment"
		url, err = actions.Comment(ctx, target, commentBody(st))
	case "github.set_status":
		s := StatusUpdate{
			State:       withString(st, "state"),
			Context:     withString(st, "context"),
			Description: withString(st, "description"),
		}
		act.Action, act.Ref = "set_status", s.State
		url, err = actions.SetStatus(ctx, target, s)
	default:
		return nil
	}
	act.URL = url
	if err != nil {
		act.Outcome, act.Error = "failed", err.Error()
		_ = rec.GitHubAction(act) //nolint:errcheck // record the failure; the original error is what we report
		return fmt.Errorf("orchestrator: hook %s on %s#%d: %w", st.Uses, target.Repository, target.Number, err)
	}
	act.Outcome = "ok"
	return rec.GitHubAction(act)
}

// commentBody resolves a github.comment step's body: an explicit `body` wins; a
// named `body_template` renders a minimal deterministic line (the rich run-summary
// template catalogue is AS-149's PR lifecycle work). A step with neither still
// posts a bare acknowledgement so a comment hook never sends an empty message.
func commentBody(st spec.Step) string {
	if b := withString(st, "body"); b != "" {
		return b
	}
	if t := withString(st, "body_template"); t != "" {
		return fmt.Sprintf("Smith orchestrator: %s", t)
	}
	return "Smith orchestrator update."
}

// withString reads a string argument from a step's with map, returning "" when the
// key is absent. A non-string scalar (an unquoted YAML `label: 123` decodes to an
// int) is stringified with fmt.Sprint so a common formatting omission still yields
// the intended value rather than silently dropping to "".
func withString(st spec.Step, key string) string {
	if st.With == nil {
		return ""
	}
	val, ok := st.With[key]
	if !ok {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprint(val)
}
