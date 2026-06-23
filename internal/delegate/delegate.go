// Package delegate runs user-delegated subagents (AS-046, PRD §7.17): the
// `task` tool's child agents. It sits at the orchestration layer alongside the
// faces and composition roots — it may depend on the loop, providers, tools,
// session store, and cost accounting, and nothing in the inward core depends on
// it.
//
// A delegation spawns a child agent that runs its own loop over its own isolated
// event log (a real, persisted session linked to the parent), so the parent's
// context window is never consumed by the child's intermediate work. The child's
// final answer is summarized back to the parent as the task tool's result, and
// the child's token usage is rolled up into the parent's session log as a
// sidechain so `/cost` and the budget guard account for the full spend.
//
// The heavy wiring lives here precisely so the task tool stays a pure leaf: the
// tool depends only on the small builtin.Spawner seam this package satisfies.
package delegate

import (
	"context"
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/schema"
)

// producer labels the usage events this package rolls up onto the parent log, so
// cost attribution can tell a delegated child's spend from the parent's own.
const producer = "task"

// Parent is the live parent-session context a delegation runs against, read
// fresh at spawn time so a mid-session model/provider/permission change (or a
// /clear or /resume that swaps the session) is reflected. The composition root
// supplies it through a closure under its controller lock.
type Parent struct {
	// Log is the parent session's event log; the child's usage is rolled up onto
	// it as a sidechain so the parent's /cost and budget account for it.
	Log *eventlog.Log
	// SessionID is the parent session, recorded as the child's parent link and on
	// the rolled-up usage's thread.
	SessionID string
	// ProvName selects the provider (vendor key) the child uses.
	ProvName string
	// Model is the parent's current model, the fallback when no cheap tier or
	// explicit override resolves.
	Model string
	// Permission is the parent's permission gate; the child inherits it so a
	// child's tool call prompts through the same parent UI (D-CLI-9 / AS-016). May
	// be nil (no gate wired, e.g. tests).
	Permission tool.PermissionFunc
	// Router resolves the cheap model tier for fan-out (AS-042); zero value falls
	// back to the parent model.
	Router routing.Policy
}

// Spawner builds and runs child agents. Construct it with New and pass it to
// builtin.NewTask. It is safe for concurrent use: each Spawn builds its own
// child session, runtime, and engine, sharing only the immutable provider map
// and the (mutex-guarded) parent log.
type Spawner struct {
	store      *session.Store
	providers  map[string]provider.Provider
	childTools func() (*tool.Registry, error)
	parent     func() Parent
}

// New builds a Spawner.
//
//   - store creates the child sessions (linked to the parent).
//   - providers is the vendor→provider map the child draws from.
//   - childTools builds the child's tool registry on each spawn. It must NOT
//     include the task tool, so delegation does not recurse.
//   - parent returns the live parent context at spawn time.
//
// The child's token usage is rolled up onto the parent log as exact counts; the
// parent's existing /cost path prices it, so this package needs no pricing table.
func New(
	store *session.Store,
	providers map[string]provider.Provider,
	childTools func() (*tool.Registry, error),
	parent func() Parent,
) *Spawner {
	return &Spawner{
		store:      store,
		providers:  providers,
		childTools: childTools,
		parent:     parent,
	}
}

// Spawn runs one delegation to completion and returns its summary. It satisfies
// builtin.Spawner.
func (s *Spawner) Spawn(ctx context.Context, req builtin.TaskRequest) (builtin.TaskResult, error) {
	p := s.parent()
	prov, ok := s.providers[p.ProvName]
	if !ok {
		return builtin.TaskResult{}, fmt.Errorf("delegate: no provider configured for %q", p.ProvName)
	}
	model := resolveModel(req.Model, p)

	child, err := s.store.CreateChild(taskTitle(req.Prompt), p.SessionID)
	if err != nil {
		return builtin.TaskResult{}, fmt.Errorf("delegate: create child session: %w", err)
	}
	// Roll up usage in the defer, before closing, so spend the child already
	// incurred is accounted for on the parent log even when the run errors or is
	// cancelled partway through.
	defer func() {
		rollUpUsage(p.Log, child.Log.Events(), child.ID, p.SessionID)
		_ = child.Log.Close()
	}()

	reg, err := s.childTools()
	if err != nil {
		return builtin.TaskResult{}, fmt.Errorf("delegate: build child tools: %w", err)
	}

	var rtOpts []tool.Option
	if p.Permission != nil {
		rtOpts = append(rtOpts, tool.WithPermission(p.Permission))
	}
	rt := tool.NewRuntime(reg, child.Log, rtOpts...)

	eng, err := loop.New(prov, child.Log, rt, reg, model)
	if err != nil {
		return builtin.TaskResult{}, fmt.Errorf("delegate: build child engine: %w", err)
	}

	res, err := eng.Run(ctx, req.Prompt)
	if err != nil {
		return builtin.TaskResult{}, err
	}
	return builtin.TaskResult{Summary: res.FinalText, SessionID: child.ID}, nil
}

// resolveModel picks the child's model: an explicit per-call override wins;
// otherwise the cheap routing tier for the parent's provider keeps fan-out
// inexpensive (PRD §7.17); failing that, the parent's current model.
func resolveModel(override string, p Parent) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return p.Router.Resolve(routing.Cheap, p.ProvName, p.Model)
}

// rollUpUsage copies each of the child's usage events onto the parent log as a
// sidechain (AS-046), so the parent's /cost totals and budget guard include the
// delegated spend, itemizable by the child's agent ID. It is best-effort: a
// rollup append that fails does not fail the delegation, whose authoritative
// record is the child session itself.
func rollUpUsage(parentLog *eventlog.Log, childEvents []schema.Block, childID, parentID string) {
	if parentLog == nil {
		return
	}
	for _, b := range childEvents {
		if b.Kind != eventlog.KindUsage {
			continue
		}
		vendor, model := "", ""
		if b.Provider != nil {
			vendor, model = b.Provider.Vendor, b.Provider.Model
		}
		u := eventlog.NewUsage(producer, vendor, model, b.StopReason, b.Tokens, b.UsageMeta)
		u.Thread = &schema.Thread{AgentID: childID, ParentThreadID: parentID, IsSidechain: true}
		u.Attribution = &schema.Attribution{Tool: "task"}
		_, _ = parentLog.Append(u)
	}
}

// taskTitle derives a short, human-readable session title from the prompt.
func taskTitle(prompt string) string {
	t := strings.TrimSpace(strings.ReplaceAll(prompt, "\n", " "))
	const max = 60
	if len(t) > max {
		t = strings.TrimSpace(t[:max]) + "…"
	}
	if t == "" {
		return "task"
	}
	return "task: " + t
}
