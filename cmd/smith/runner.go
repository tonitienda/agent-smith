package main

import (
	"context"
	"fmt"
	"io"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/delegate"
	"github.com/tonitienda/agent-smith/internal/hook"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/schema"
)

// runOutcome is the face-neutral result of one headless execution: the session it
// captured (so it is `/resume`-able, AS-051 AC4), the loop result, the priced
// total, the calls the permission posture denied, and the error the run returned
// (nil on a clean stop). It is what both `smith run` rendering and the background
// runner's bookkeeping (AS-054) are built from, so the two faces classify an
// outcome identically.
type runOutcome struct {
	sessionID string
	res       loop.Result
	costUSD   float64
	denied    []deniedCall
	runErr    error
}

// executeRun is the shared headless execution core (AS-051, AS-054). It builds a
// fresh session — resumable later — seeds memory files (AS-032), wires the
// allowlist-then-deny permission posture (D-CLI-9, or auto with opts.auto), the
// lifecycle hooks (AS-035), capture-time redaction (AS-115), the `task` delegation
// tool (AS-046/AS-119), budget enforcement (AS-041/AS-086) when opts.budgetUSD is
// set, and the passive sub-agent analyzers (AS-107), then runs a single turn. obs
// is an optional loop observer for streaming faces (nil for the background runner);
// stderr carries hook diagnostics. The returned error is a *setup* failure; a
// failed run surfaces in runOutcome.runErr so the caller can classify it.
func executeRun(ctx context.Context, configOverride, wd, prompt string, opts headlessOpts, obs loop.Observer, stderr io.Writer) (runOutcome, error) {
	store, err := session.NewStore("", wd)
	if err != nil {
		return runOutcome{}, fmt.Errorf("open session store: %w", err)
	}
	sess, err := store.Create("")
	if err != nil {
		return runOutcome{}, fmt.Errorf("create session: %w", err)
	}
	defer func() { _ = sess.Log.Close() }()
	if err := seedMemory(wd, sess); err != nil {
		return runOutcome{}, err
	}

	tools, err := appRuntime.BuiltinTools(wd)
	if err != nil {
		return runOutcome{}, err
	}
	prov, provName, model, err := headlessProvider(configOverride)
	if err != nil {
		return runOutcome{}, err
	}
	pricing, err := sessionPricing(configOverride)
	if err != nil {
		return runOutcome{}, fmt.Errorf("load pricing table: %w", err)
	}

	gate, err := headlessPermission(wd, configOverride, opts.auto)
	if err != nil {
		return runOutcome{}, fmt.Errorf("load permission policy: %w", err)
	}

	cfg, err := loadLayeredConfig(configOverride)
	if err != nil {
		return runOutcome{}, fmt.Errorf("load config: %w", err)
	}
	hooks := loadHooks(cfg, stderr)
	applyRedaction(cfg, sess.Log, stderr)

	router, _ := routing.ConfigFrom(cfg)
	childBudgetCfg, _ := budget.ConfigFrom(cfg)
	taskParent := func() delegate.Parent {
		return delegate.Parent{
			Log:                sess.Log,
			SessionID:          sess.ID,
			ProvName:           provName,
			Model:              model,
			Permission:         gate.decide,
			Router:             router,
			Pricing:            pricing,
			ChildBudgetUSD:     childBudgetCfg.PerChildLimitUSD,
			BudgetWarnFraction: childBudgetCfg.WarnFraction,
		}
	}
	if err := tools.Register(builtin.NewTask(taskSpawner(store, wd, nil, nil, taskParent))); err != nil {
		return runOutcome{}, fmt.Errorf("register task tool: %w", err)
	}

	rtOpts := append([]tool.Option{tool.WithPermission(gate.decide)}, hookToolOptions(hooks, sess.Log, sess.ID)...)
	rt := tool.NewRuntime(tools, sess.Log, rtOpts...)

	engOpts := []loop.Option{}
	if obs != nil {
		engOpts = append(engOpts, loop.WithObserver(obs))
	}
	if opts.budgetUSD > 0 {
		log := sess.Log
		spent := func() float64 { return cost.Summarize(log.Events(), pricing).TotalUSD }
		reserve := func(c []schema.Block) (float64, bool) { return cost.EstimateTurnCostUSD(c, model, pricing) }
		engOpts = append(engOpts,
			loop.WithBudget(spent, opts.budgetUSD, 0),
			loop.WithBudgetReservation(reserve, false),
		)
	}
	subReg, subStore, err := buildSubAgents(cfg, store, saveTargetResolver(wd, nil), openFactLedger(store, stderr), nil, nil, stderr)
	if err != nil {
		return runOutcome{}, fmt.Errorf("build sub-agents: %w", err)
	}
	engOpts = append(engOpts, loop.WithSubAgents(subagent.NewRunner(subReg, subStore, sess.ID)))
	eng, err := loop.New(prov, sess.Log, rt, tools, model, engOpts...)
	if err != nil {
		return runOutcome{}, fmt.Errorf("build engine: %w", err)
	}

	fireLifecycle(ctx, hooks, sess.Log, hook.Payload{Event: hook.SessionStart, Session: sess.ID})
	defer fireLifecycle(context.Background(), hooks, sess.Log, hook.Payload{Event: hook.SessionStop, Session: sess.ID})

	if out := fireLifecycle(ctx, hooks, sess.Log, hook.Payload{Event: hook.UserPromptSubmit, Session: sess.ID, Prompt: prompt}); out.Blocked {
		return runOutcome{}, fmt.Errorf("prompt blocked by hook: %s", out.Reason)
	} else if rewritten := promptRewrite(out.Input); rewritten != "" {
		prompt = rewritten
	}

	res, runErr := eng.Run(ctx, prompt)
	totalUSD := cost.Summarize(sess.Log.Events(), pricing).TotalUSD
	// Persist the replay manifest and (if configured) export the OpenTelemetry
	// trace (AS-055). A fresh context so a Ctrl+C-canceled run still records its
	// manifest; failures are warned, never fatal, to the run.
	persistRunArtifacts(context.Background(), sess, cfg, pricing, stderr)
	return runOutcome{sessionID: sess.ID, res: res, costUSD: totalUSD, denied: gate.denied(), runErr: runErr}, nil
}
