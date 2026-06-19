package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/clean"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/compact"
	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/goal"
	"github.com/tonitienda/agent-smith/internal/hook"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/permission"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/rewind"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tui"
	"github.com/tonitienda/agent-smith/schema"
)

// chatSession owns the mutable per-session state that the parity commands
// (AS-023: /clear, /model, /resume) reshape mid-run, and presents it to the TUI
// through stable, face-agnostic seams: Run drives a turn (tui.Runner), Meta
// feeds the status-line identity (tui.MetaFunc), and Meter feeds the context
// gauge (tui.MeterFunc). The TUI holds those three closures, never this struct,
// so a command can swap the active provider/model or the whole session and the
// face simply re-reads the seams — keeping internal/tui free of the
// provider/tool/session wiring (the AS-021 boundary).
//
// One engine drives one session against one provider; switching either rebuilds
// the engine over the (possibly new) log, re-using the same UIEvent observer so
// turn progress keeps flowing to the face. The log is the single source of
// truth, so a switch is just a new engine over a log — no in-loop model state to
// migrate.
type chatSession struct {
	store     *session.Store
	tools     *tool.Registry
	pricing   *cost.Table
	providers map[string]provider.Provider // vendor -> provider
	observer  loop.Observer
	// policy is the permission gate every tool call passes through (AS-016/AS-024).
	// It is built once and reused across engine rebuilds (/clear, /model, /resume)
	// so a remembered "always allow" rule keeps applying for the rest of the
	// session. It may be nil (tests), in which case no gate is wired.
	policy *permission.Policy

	// project labels the working context in the startup header (D-TUI-10); it is
	// static for the session's lifetime, so it needs no lock.
	project string
	// wd is the working directory the session runs in; it is the root for memory
	// file discovery (AS-032) when a fresh session is created mid-run (/clear).
	wd string
	// skills is the portable-skill snapshot scanned once at startup (AS-034). It
	// builds the skill tool and seeds skill_load events, so a fresh session created
	// mid-run (/clear) records exactly the catalog the tool offers — they cannot
	// diverge from an in-flight filesystem change.
	skills []skill.Skill
	// hooks is the lifecycle-hook set (AS-035) loaded once from config at startup.
	// buildEngine wires its pre/post-tool-use hooks into every runtime, and Run /
	// start fire its prompt-submit and session lifecycle events. A nil-safe *Set
	// fires nothing, so a hook-free session behaves exactly as before.
	hooks *hook.Set

	// budgetDefaultUSD and budgetWarnFraction are the configured budget defaults
	// (AS-041) read once from layered config at startup: the default ceiling
	// applied when the session log carries no /budget override, and the fraction
	// of the ceiling at which warnings begin. Both are static for the session.
	budgetDefaultUSD   float64
	budgetWarnFraction float64

	// budgetHaltUnpriced (AS-086) decides whether a budgeted session halts rather
	// than spending blind when the active model has no pricing entry (so the guard
	// cannot enforce the ceiling against it). Default false: warn once, proceed.
	budgetHaltUnpriced bool

	// autoCompact (AS-085) enables compacting the older span before a turn when the
	// projected window crosses autoCompactThreshold (a fraction of the model's
	// context window). Off by default — the product prefers /clean and /tidy; this
	// is the blunt last-resort guard against a context-window-exceeded stop.
	// autoCompactThreshold is the trigger fraction; it falls back to a default when
	// the flag is on but no threshold is configured (or it is out of (0,1)).
	autoCompact          bool
	autoCompactThreshold float64

	mu       sync.Mutex
	sess     *session.Session
	provName string
	model    string
	engine   *loop.Engine

	// pendingClean holds the previewed /clean plan awaiting confirmation
	// (/clean --apply) or discard (/clean --cancel). nil when none is pending.
	// It is keyed to the session it was previewed against, so a /clear or
	// /resume between preview and apply invalidates it rather than removing
	// blocks from the wrong log.
	pendingClean    *clean.Plan
	pendingCleanFor *session.Session

	// pendingRewind holds the previewed /rewind plan awaiting confirmation
	// (/rewind --apply) or discard (/rewind --cancel), keyed to the session it was
	// previewed against so a /clear or /resume between preview and apply
	// invalidates it rather than rewinding the wrong log (mirrors pendingClean).
	pendingRewind    *rewind.Plan
	pendingRewindFor *session.Session

	// pendingCompact holds the previewed /compact plan awaiting confirmation
	// (/compact --apply) or discard (/compact --cancel), keyed to the session it
	// was previewed against so a /clear or /resume between preview and apply
	// invalidates it rather than compacting the wrong log (mirrors pendingClean).
	pendingCompact    *compact.Plan
	pendingCompactFor *session.Session

	// meter memo: recomputed only when the active log, its length, or the model
	// changes, so the per-delta status-line refresh stays O(1) (mirrors AS-025).
	meterLog   *eventlog.Log
	meterLen   int
	meterModel string
	meterCache tui.Meter

	// goal memo: the active session objective (AS-040), recomputed only when the
	// active log or its length changes. Meta() runs on every token delta (via
	// refreshMeter), so without this the goal projection would re-run dozens of
	// times a second; this keeps that refresh O(1) like the meter.
	goalLog   *eventlog.Log
	goalLen   int
	goalCache string
}

// newChatSession builds the controller over an already-opened session, wiring
// the default Anthropic + OpenAI providers and the model for the first turn. The
// engine is not built yet: the caller sets the observer (from the TUI) and calls
// start so turn progress is wired before the first turn runs.
func newChatSession(store *session.Store, tools *tool.Registry, pricing *cost.Table, providers map[string]provider.Provider, sess *session.Session, provName, model, wd string, skills []skill.Skill, hooks *hook.Set) *chatSession {
	return &chatSession{
		store:     store,
		tools:     tools,
		pricing:   pricing,
		providers: providers,
		sess:      sess,
		provName:  provName,
		model:     model,
		project:   filepath.Base(wd),
		wd:        wd,
		skills:    skills,
		hooks:     hooks,
	}
}

// setBudgetDefaults records the configured budget ceiling default and warning
// fraction (AS-041), read from layered config at startup. A non-positive default
// leaves new sessions budget-free until /budget sets one; a warn fraction outside
// (0,1) falls back to the budget package default downstream.
func (s *chatSession) setBudgetDefaults(defaultUSD, warnFraction float64, haltUnpriced bool) {
	s.budgetDefaultUSD = defaultUSD
	s.budgetWarnFraction = warnFraction
	s.budgetHaltUnpriced = haltUnpriced
}

// defaultAutoCompactThreshold is the window fraction auto-compaction triggers at
// when enabled without an explicit (or in-range) compact.auto_threshold. 0.85
// leaves headroom for the turn's own output before the window limit.
const defaultAutoCompactThreshold = 0.85

// setAutoCompact records the auto-compaction config (AS-085), read once from
// layered config at startup. enabled off (default) is a no-op at turn time. A
// threshold outside (0,1) falls back to defaultAutoCompactThreshold so a stray 0
// or 1 cannot disable the guard or make it fire every turn.
func (s *chatSession) setAutoCompact(enabled bool, threshold float64) {
	s.autoCompact = enabled
	if threshold <= 0 || threshold >= 1 {
		threshold = defaultAutoCompactThreshold
	}
	s.autoCompactThreshold = threshold
}

// setPolicy installs the permission gate used by every engine this controller
// builds. It must be called before start (and before any turn), so the first
// engine already carries the gate.
func (s *chatSession) setPolicy(p *permission.Policy) { s.policy = p }

// start records the UIEvent observer and builds the initial engine. It must be
// called once, after the TUI exists (so its observer is available) and before
// the first turn.
func (s *chatSession) start(observer loop.Observer) error {
	s.observer = observer
	eng, err := s.buildEngine(s.sess, s.provName, s.model)
	if err != nil {
		return err
	}
	s.engine = eng
	// Fire the session-start lifecycle hook (AS-035) once the session is wired.
	fireLifecycle(context.Background(), s.hooks, s.sess.Log, hook.Payload{
		Event:   hook.SessionStart,
		Session: s.sess.ID,
	})
	return nil
}

// stop fires the session-stop lifecycle hook (AS-035). The chat face calls it as
// the app shuts down, so a hook can run teardown (flush, notify) with the final
// log in place. It is best-effort: a stop hook cannot block an exit.
func (s *chatSession) stop() {
	s.mu.Lock()
	log, id := s.sess.Log, s.sess.ID
	s.mu.Unlock()
	fireLifecycle(context.Background(), s.hooks, log, hook.Payload{
		Event:   hook.SessionStop,
		Session: id,
	})
}

// buildEngine constructs an engine over the given session log, provider, and
// model without mutating any controller state. A switch builds the new engine
// first and only commits the new session/provider/model fields once this
// succeeds, so a build failure leaves the controller on its previous, consistent
// state rather than half-switched.
func (s *chatSession) buildEngine(sess *session.Session, provName, model string) (*loop.Engine, error) {
	prov, ok := s.providers[provName]
	if !ok {
		return nil, fmt.Errorf("no provider configured for %q", provName)
	}
	var rtOpts []tool.Option
	if s.policy != nil {
		rtOpts = append(rtOpts, tool.WithPermission(s.policy.Func()))
	}
	// Wire the pre/post-tool-use lifecycle hooks (AS-035) into this engine's
	// runtime; they run after the permission gate, never replacing it.
	rtOpts = append(rtOpts, hookToolOptions(s.hooks, sess.Log, sess.ID)...)
	rt := tool.NewRuntime(s.tools, sess.Log, rtOpts...)
	// Budget enforcement (AS-041): spend is the session's running dollar total
	// from the same accounting source as /cost and the meter, so the guard, the
	// command, and the gauge never drift. Pricing may be nil (cli/tests), in which
	// case enforcement is left disabled.
	opts := []loop.Option{loop.WithObserver(s.observer)}
	if s.pricing != nil {
		log := sess.Log
		spent := func() float64 { return cost.Summarize(log.Events(), s.pricing).TotalUSD }
		opts = append(opts, loop.WithBudget(spent, s.budgetDefaultUSD, s.budgetWarnFraction))
		// Conservative pre-turn enforcement (AS-086): reserve the next turn's
		// worst-case cost (request-size input + the model's max output) so spend
		// cannot overshoot the ceiling, and surface an unpriced model the guard
		// cannot enforce. Priced against the same table and model as /cost.
		reserveModel := model
		reserve := func(ctx []schema.Block) (float64, bool) {
			return cost.EstimateTurnCostUSD(ctx, reserveModel, s.pricing)
		}
		opts = append(opts, loop.WithBudgetReservation(reserve, s.budgetHaltUnpriced))
	}
	return loop.New(prov, sess.Log, rt, s.tools, model, opts...)
}

// Run drives one user turn against the current engine (tui.Runner). It reads the
// engine under the lock and releases it before the turn so a long turn does not
// serialize the status-line meter or a concurrent command dispatch.
func (s *chatSession) Run(ctx context.Context, userText string) (loop.Result, error) {
	s.mu.Lock()
	eng := s.engine
	log := s.sess.Log
	id := s.sess.ID
	s.mu.Unlock()

	// Fire the user-prompt-submit hook (AS-035) before the prompt is recorded: it
	// may block the turn (the prompt is rejected, the model never sees it) or
	// rewrite the prompt text the model receives.
	if s.hooks.Has(hook.UserPromptSubmit) {
		out := fireLifecycle(ctx, s.hooks, log, hook.Payload{
			Event:   hook.UserPromptSubmit,
			Session: id,
			Prompt:  userText,
		})
		if out.Blocked {
			return loop.Result{StopReason: "hook_blocked"}, fmt.Errorf("prompt blocked by hook: %s", out.Reason)
		}
		if len(out.Input) > 0 {
			if rewritten := promptRewrite(out.Input); rewritten != "" {
				userText = rewritten
			}
		}
	}
	// Auto-compact the older span before the turn if the projected window is over
	// the configured threshold (AS-085), so a near-full window does not push the
	// turn into a context-window-exceeded stop. Best-effort: any failure surfaces a
	// notice and the turn proceeds, never blocking on the blunt-instrument guard.
	s.maybeAutoCompact(ctx)
	return eng.Run(ctx, userText)
}

// maybeAutoCompact runs one compaction before the turn when auto-compaction is
// enabled and the projected window has crossed the configured threshold (AS-085).
// It reuses the manual /compact engine unchanged — same Preview → cheap-tier
// summarize → Build path — so the result is the same reversible compaction block
// (/compact --undo restores it) and its cost is itemized in /cost, attributed to
// AutoUsageProducer so it reads distinctly from a user-invoked /compact. The
// compaction is surfaced via UIAutoCompact (D0: never silent). The lock is held
// only around log snapshots/appends, released across the projection and model
// call (mirrors compactApply).
func (s *chatSession) maybeAutoCompact(ctx context.Context) {
	s.mu.Lock()
	if !s.autoCompact {
		s.mu.Unlock()
		return
	}
	events := s.sess.Log.Events()
	model, pricing, sess := s.model, s.pricing, s.sess
	threshold := s.autoCompactThreshold
	s.mu.Unlock()

	proj := projection.Project(events, projection.Options{TargetModel: model})
	comp := composition.Build(proj, pricing, model, time.Now(), composition.SortSize)
	// Window unknown (unpriced/unlisted model): the threshold is a fraction of it,
	// so without it the guard cannot be evaluated — leave the turn alone.
	if comp.Window <= 0 || float64(comp.TotalTokens) < threshold*float64(comp.Window) {
		return
	}

	plan := compact.Preview(proj, pricing, model, time.Now())
	if plan.Empty() {
		return // nothing older than the recent turn to compact; let the turn proceed
	}

	s.mu.Lock()
	if s.sess != sess {
		s.mu.Unlock()
		return // /clear or /resume landed mid-projection; abandon this stale plan
	}
	vendor, cheap := s.cheapModel()
	prov := s.providers[vendor]
	log, sessID := s.sess.Log, s.sess.ID
	s.mu.Unlock()
	if prov == nil {
		return
	}

	// Fire the pre-compact hook (AS-035) with the auto reason so a policy can
	// distinguish (and veto) machine-triggered compaction from /compact.
	if out := fireLifecycle(ctx, s.hooks, log, hook.Payload{
		Event:   hook.PreCompact,
		Session: sessID,
		Reason:  "auto",
	}); out.Blocked {
		return
	}

	summary, tokens, stopReason, err := summarize(ctx, prov, cheap, plan.Sources)
	if err != nil {
		// A cancelled turn (user interrupt) surfaces here as a summarize error;
		// the turn itself is already being aborted, so a failure notice would only
		// confuse. Surface only a genuine failure on a still-live context.
		if ctx.Err() == nil {
			s.emit(loop.UIEvent{Kind: loop.UIAutoCompact, Text: "auto-compact failed: " + err.Error() + " — continuing with the full context."})
		}
		return
	}
	block, ok := compact.Build(plan, summary)
	if !ok {
		return
	}

	// Append under the lock, then release it before emitting: the observer is an
	// external callback that may re-enter the controller and re-acquire s.mu, so
	// holding the lock across it risks a deadlock.
	s.mu.Lock()
	if s.sess != sess {
		s.mu.Unlock()
		return // session swapped during the model call; do not append to the new log
	}
	if tokens != nil {
		if _, err := s.sess.Log.Append(eventlog.NewUsage(compact.AutoUsageProducer, vendor, cheap, stopReason, tokens, nil)); err != nil {
			s.mu.Unlock()
			return
		}
	}
	if _, err := s.sess.Log.Append(block); err != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	s.emit(loop.UIEvent{Kind: loop.UIAutoCompact, Text: fmt.Sprintf(
		"auto-compacted %s into one summary (~%d tokens) as the context neared the window limit. Restore with /compact --undo.",
		pluralBlocks(len(plan.SourceIDs)), plan.Tokens)})
}

// emit delivers a UIEvent to the face observer if one is wired (nil in tests).
func (s *chatSession) emit(ev loop.UIEvent) {
	if s.observer != nil {
		s.observer(ev)
	}
}

// promptRewrite extracts a rewritten prompt string from a user-prompt-submit
// hook's modification. A hook returns the new prompt as a JSON string in its
// `input` field; a non-string (or empty) value leaves the prompt unchanged.
func promptRewrite(raw []byte) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// Meta reports the current status-line identity (tui.MetaFunc).
func (s *chatSession) Meta() tui.Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return tui.Meta{
		Provider: s.provName,
		Model:    s.model,
		Session:  shortID(s.sess.ID),
		Project:  s.project,
		Goal:     s.currentGoal(),
	}
}

// currentGoal returns the active session objective for the status line (AS-040),
// memoized on the active log and its length so the per-delta status refresh
// (refreshMeter → Meta) stays O(1); the goal projection only re-runs when the
// log grows or is swapped (/clear, /resume). Callers must hold s.mu.
func (s *chatSession) currentGoal() string {
	log := s.sess.Log
	if log == s.goalLog && log.Len() == s.goalLen {
		return s.goalCache
	}
	objective := ""
	if g, ok := goal.Current(log.Events()); ok {
		objective = g.Objective
	}
	s.goalLog, s.goalLen, s.goalCache = log, log.Len(), objective
	return objective
}

// cmdGoal sets, shows, or completes the session objective (AS-040 /goal). The
// goal lives on the event log as a model-facing block (D3): setting it appends a
// goal block, replacing it or `/goal done` retires the prior goal with an
// exclusion, and the whole history is reconstructable from events — so /insights
// (AS-045) reads it straight from the session with no separate stored state.
//
//   - /goal "<objective>"  set (or replace) the session goal
//   - /goal                show the current goal and its history
//   - /goal done           mark the goal complete (retires it from the window)
func (s *chatSession) cmdGoal(_ context.Context, args []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.sess.Log.Events()

	if len(args) == 0 {
		return command.Output{Text: goal.Render(events)}, nil
	}

	if len(args) == 1 && strings.EqualFold(args[0], "done") {
		cur, ok := goal.Current(events)
		if !ok {
			return command.Output{Text: "No active goal to complete."}, nil
		}
		if _, err := s.sess.Log.Append(goal.Retire(cur.BlockID)); err != nil {
			return command.Output{}, fmt.Errorf("record goal completion: %w", err)
		}
		return command.Output{Text: "Goal completed: " + cur.Objective}, nil
	}

	objective := strings.TrimSpace(strings.Join(args, " "))
	if objective == "" {
		return command.Output{}, fmt.Errorf(`/goal needs an objective, e.g. /goal "ship the parser"`)
	}
	// Retire the active goal first so exactly one goal stays live in the window;
	// the retired block remains on the log (history is never mutated, D3).
	if cur, ok := goal.Current(events); ok {
		if _, err := s.sess.Log.Append(goal.Retire(cur.BlockID)); err != nil {
			return command.Output{}, fmt.Errorf("retire previous goal: %w", err)
		}
	}
	if _, err := s.sess.Log.Append(goal.Set(objective)); err != nil {
		return command.Output{}, fmt.Errorf("record goal: %w", err)
	}
	return command.Output{Text: "Goal set: " + objective}, nil
}

// cmdBudget implements /budget (AS-041): set, show, or clear the session spend
// ceiling. The ceiling is recorded on the log (budget.Set), so it survives
// /resume and the loop enforces it at every turn boundary.
//
//   - /budget            show the active ceiling, warning threshold, and spend
//   - /budget <amount>   set the ceiling (a leading currency symbol is allowed)
//   - /budget off        clear the budget (records a 0 ceiling)
func (s *chatSession) cmdBudget(_ context.Context, args []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.sess.Log.Events()

	if len(args) == 0 {
		return command.Output{Text: s.renderBudget(events)}, nil
	}

	arg := strings.TrimSpace(strings.Join(args, " "))
	if strings.EqualFold(arg, "off") || strings.EqualFold(arg, "none") {
		if _, err := s.sess.Log.Append(budget.Set(0)); err != nil {
			return command.Output{}, fmt.Errorf("clear budget: %w", err)
		}
		return command.Output{Text: "Budget cleared."}, nil
	}

	limit, err := parseBudgetAmount(arg)
	if err != nil {
		return command.Output{}, err
	}
	if _, err := s.sess.Log.Append(budget.Set(limit)); err != nil {
		return command.Output{}, fmt.Errorf("record budget: %w", err)
	}
	sym := cost.Symbol(cost.Summarize(events, s.pricing).Currency)
	return command.Output{Text: fmt.Sprintf("Budget set: %s%s", sym, strconv.FormatFloat(limit, 'f', 2, 64))}, nil
}

// renderBudget describes the active budget for /budget with no arguments: the
// ceiling (a /budget override or the configured default), the warning threshold,
// and what the session has spent so far against it. Callers hold s.mu.
func (s *chatSession) renderBudget(events []schema.Block) string {
	summary := cost.Summarize(events, s.pricing)
	sym := cost.Symbol(summary.Currency)
	limit := s.budgetDefaultUSD
	if v, ok := budget.Current(events); ok {
		limit = v
	}
	g := budget.Guard{LimitUSD: limit, WarnFraction: s.budgetWarnFraction}
	if !g.Enabled() {
		return fmt.Sprintf("No budget set — spent %s%s so far. Set one with /budget <amount>.",
			sym, strconv.FormatFloat(summary.TotalUSD, 'f', 4, 64))
	}
	return fmt.Sprintf("Budget %s%s · warn at %s%s · spent %s%s (%s).",
		sym, strconv.FormatFloat(limit, 'f', 2, 64),
		sym, strconv.FormatFloat(g.WarnThresholdUSD(), 'f', 2, 64),
		sym, strconv.FormatFloat(summary.TotalUSD, 'f', 4, 64),
		g.Check(summary.TotalUSD))
}

// parseBudgetAmount parses a /budget amount, tolerating a leading currency
// symbol (e.g. "$0.50") and surrounding whitespace. A non-positive or unparseable
// amount is an error — clearing a budget goes through "/budget off", so a typo
// never silently disables enforcement.
func parseBudgetAmount(arg string) (float64, error) {
	cleaned := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(arg), "$€£"))
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, fmt.Errorf("/budget needs a dollar amount, e.g. /budget 0.50 (or /budget off)")
	}
	if v <= 0 {
		return 0, fmt.Errorf("/budget amount must be positive; use /budget off to clear")
	}
	return v, nil
}

// Meter computes the context/cost snapshot for the status line (tui.MeterFunc)
// from the current session log and pricing table — the same accounting source as
// /cost, so the gauge and the command never drift. The window denominator uses
// the model the status line passes (the active model), so it rescales the moment
// /model switches. The result is memoized on the active log, its length, and the
// model, so the per-delta refresh is an O(1) check.
func (s *chatSession) Meter(model string) tui.Meter {
	s.mu.Lock()
	defer s.mu.Unlock()
	log := s.sess.Log
	if log == s.meterLog && log.Len() == s.meterLen && model == s.meterModel {
		return s.meterCache
	}
	events := log.Events()
	summary := cost.Summarize(events, s.pricing)
	used := 0
	if last, ok := summary.Latest(); ok {
		used = last.ContextTokens()
	}
	window, _ := s.pricing.Window(model)
	// Resolve the active budget ceiling (AS-041): a /budget override on the log
	// wins over the configured default, so the gauge tracks whatever the loop
	// enforces.
	budgetUSD := s.budgetDefaultUSD
	if v, ok := budget.Current(events); ok {
		budgetUSD = v
	}
	s.meterCache = tui.Meter{
		Tokens:             used,
		Window:             window,
		CostUSD:            summary.TotalUSD,
		CostKnown:          summary.AllPriced,
		Currency:           cost.Symbol(summary.Currency),
		BudgetUSD:          budgetUSD,
		BudgetWarnFraction: s.budgetWarnFraction,
	}
	s.meterLog, s.meterLen, s.meterModel = log, len(events), model
	return s.meterCache
}

// events returns a snapshot of the current session log, for /cost.
func (s *chatSession) events() []schema.Block {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sess.Log.Events()
}

// cmdContext renders the /context composition view (AS-026): what is occupying
// the window right now, ranked by share, from the live projection alone — no
// model calls, so the panel opens instantly. The optional argument sorts the
// full segment list (size | age | type; default size). The projection uses the
// active model so reasoning-replay filtering matches the real window, and prices
// each block's estimated tokens at that model's input rate.
func (s *chatSession) cmdContext(_ context.Context, args []string) (command.Output, error) {
	s.mu.Lock()
	events := s.sess.Log.Events()
	model := s.model
	table := s.pricing
	s.mu.Unlock()

	sortBy := composition.SortSize
	if len(args) > 0 {
		sortBy = composition.ParseSort(args[0])
	}
	proj := projection.Project(events, projection.Options{TargetModel: model})
	comp := composition.Build(proj, table, model, time.Now(), sortBy)
	return command.Output{Text: composition.Render(comp)}, nil
}

// cmdClean is the manual context editor (AS-028 /clean, PRD §7.12): the user
// selects live segments by their /context handle, sees a preview of exactly what
// leaves the window and the tokens/$ reclaimed, then confirms. Removal is an
// appended exclusion event — history is never mutated (D3) — and /clean --undo
// restores the most recent removal exactly.
//
//   - /clean <handle>…  preview the removal (mutates nothing) and stage it
//   - /clean --apply     confirm the staged preview, appending the exclusion
//   - /clean --undo      restore the most recent removal
//   - /clean --cancel    discard the staged preview
func (s *chatSession) cmdClean(_ context.Context, args []string) (command.Output, error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--apply":
			return s.cleanApply()
		case "--undo":
			return s.cleanUndo()
		case "--cancel":
			return s.cleanCancel()
		default:
			return command.Output{}, fmt.Errorf("unknown /clean flag %q (use --apply, --undo, or --cancel)", args[0])
		}
	}
	if len(args) == 0 {
		// No args: show the usage text for a scriptable face, and offer the
		// interactive multi-select surface for an interactive one (AS-068). The TUI
		// opens the selector; the CLI ignores it and renders the usage text.
		return command.Output{Text: cleanUsage, Selector: s.buildCleanSelector()}, nil
	}
	return s.cleanPreview(args)
}

// buildCleanSelector snapshots the live composition into the interactive
// multi-select surface (AS-068): the live segments become selectable items
// (largest first, so the biggest consumers lead), the excluded blocks become the
// restorable archive, and the Preview/Apply/Restore closures run the same
// projection + clean engine as the typed /clean path — so the in-panel
// selection and `/clean <handle>` can never disagree about what a removal does.
func (s *chatSession) buildCleanSelector() *command.Selector {
	s.mu.Lock()
	events := s.sess.Log.Events()
	model := s.model
	table := s.pricing
	s.mu.Unlock()

	proj := projection.Project(events, projection.Options{TargetModel: model})
	comp := composition.Build(proj, table, model, time.Now(), composition.SortSize)

	items := make([]command.SelectItem, 0, len(comp.Segments))
	for _, seg := range comp.Segments {
		items = append(items, command.SelectItem{Label: selectLabel(seg), Value: seg.ID})
	}
	archive := make([]command.SelectItem, 0, len(comp.Excluded))
	for _, seg := range comp.Excluded {
		archive = append(archive, command.SelectItem{Label: selectLabel(seg), Value: seg.ID})
	}
	return &command.Selector{
		Title:   "Clean the context window",
		Items:   items,
		Archive: archive,
		Preview: s.cleanSelectPreview,
		Apply:   s.cleanSelectApply,
		Restore: s.cleanSelectRestore,
	}
}

// selectLabel formats one segment as a fixed-width row for the selector list,
// showing the handle, display group, origin and estimated token share.
func selectLabel(seg composition.Segment) string {
	return fmt.Sprintf("%-13s %-13s %-24s %6d tok",
		composition.Handle(seg.ID), seg.Group, clip(seg.Origin, 24), seg.Tokens)
}

// clip shortens s to at most n runes, ending in an ellipsis when it was longer,
// so a long path or tool name can't stretch the selector rows out of alignment.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// cleanSelectPreview computes the live reclaim feedback for the selector's
// current selection (AS-068): it runs the same clean.Preview the typed path
// does, so the tokens/$ shown as the selection changes match exactly, including
// atomic tool-call/result pairing. It mutates nothing.
func (s *chatSession) cleanSelectPreview(values []string) command.SelectPreview {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(values) == 0 {
		return command.SelectPreview{Summary: "Nothing selected"}
	}
	proj := projection.Project(s.sess.Log.Events(), projection.Options{TargetModel: s.model})
	plan := clean.Preview(proj, s.pricing, s.model, time.Now(), values)
	if plan.Empty() {
		return command.SelectPreview{Summary: "Nothing selected"}
	}
	summary := fmt.Sprintf("Selected %s · %d tok reclaimed", pluralSegments(len(plan.Items)), plan.Tokens)
	if plan.Priced {
		summary += fmt.Sprintf(" (%s%s)", plan.Currency, strconv.FormatFloat(plan.CostUSD, 'f', 4, 64))
	}
	return command.SelectPreview{Summary: summary, Warnings: plan.Warnings}
}

// cleanSelectApply commits the selector's checked items as one removal through
// the existing clean.Apply path — a single appended exclusion event (AS-068 AC2)
// — and returns the result line the face surfaces.
func (s *chatSession) cleanSelectApply(values []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	proj := projection.Project(s.sess.Log.Events(), projection.Options{TargetModel: s.model})
	plan := clean.Preview(proj, s.pricing, s.model, time.Now(), values)
	event, ok := clean.Apply(plan)
	if !ok {
		return "Nothing to remove — no live segment matched the selection."
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return "Couldn't apply the removal: " + err.Error()
	}
	return fmt.Sprintf("Removed %s from the window, reclaiming %d tokens. Restore with /clean --undo.",
		pluralSegments(len(plan.Items)), plan.Tokens)
}

// cleanSelectRestore re-includes a single excluded block from the archive
// (AS-068 AC3) through clean.RestoreBlock, leaving every other removal in place.
func (s *chatSession) cleanSelectRestore(value string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	restore, ok := clean.RestoreBlock(s.sess.Log.Events(), value)
	if !ok {
		return "That block isn't excluded, so there's nothing to restore."
	}
	for _, e := range restore {
		if _, err := s.sess.Log.Append(e); err != nil {
			return "Couldn't restore the block: " + err.Error()
		}
	}
	return "Restored 1 segment to the window."
}

// cleanPreview stages a removal: it projects the live window, builds the plan,
// and stores it pending confirmation. Nothing is appended to the log.
func (s *chatSession) cleanPreview(handles []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proj := projection.Project(s.sess.Log.Events(), projection.Options{TargetModel: s.model})
	plan := clean.Preview(proj, s.pricing, s.model, time.Now(), handles)
	if plan.Empty() {
		s.pendingClean, s.pendingCleanFor = nil, nil
		return command.Output{Text: clean.RenderPreview(plan)}, nil
	}
	s.pendingClean, s.pendingCleanFor = &plan, s.sess
	return command.Output{Text: clean.RenderPreview(plan)}, nil
}

// cleanApply confirms the staged preview, appending the exclusion event that
// drops its blocks from the projection. The plan is discarded once applied.
func (s *chatSession) cleanApply() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingClean == nil {
		return command.Output{Text: "Nothing staged. Run /clean <handle>… to preview a removal first."}, nil
	}
	if s.pendingCleanFor != s.sess {
		s.pendingClean, s.pendingCleanFor = nil, nil
		return command.Output{Text: "The staged preview was for a different session and is no longer valid. Run /clean again."}, nil
	}
	plan := *s.pendingClean
	event, ok := clean.Apply(plan)
	if !ok {
		s.pendingClean, s.pendingCleanFor = nil, nil
		return command.Output{Text: "Nothing to apply."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record exclusion: %w", err)
	}
	s.pendingClean, s.pendingCleanFor = nil, nil
	return command.Output{Text: fmt.Sprintf("Removed %s from the window, reclaiming %d tokens. Restore with /clean --undo.",
		pluralSegments(len(plan.Items)), plan.Tokens)}, nil
}

// cleanUndo restores the most recent /clean removal by appending a
// counter-exclusion. The log is never rewritten, so the restoration is exact.
func (s *chatSession) cleanUndo() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, removed, ok := clean.Undo(s.sess.Log.Events())
	if !ok {
		return command.Output{Text: "No /clean removal to undo in this session."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record undo: %w", err)
	}
	return command.Output{Text: fmt.Sprintf("Restored %s to the window.", pluralSegments(removed))}, nil
}

// cleanCancel discards a staged preview without touching the log.
func (s *chatSession) cleanCancel() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingClean == nil {
		return command.Output{Text: "Nothing staged to cancel."}, nil
	}
	s.pendingClean, s.pendingCleanFor = nil, nil
	return command.Output{Text: "Discarded the staged /clean preview. Nothing changed."}, nil
}

// pluralSegments labels a segment count for the confirm/undo lines.
func pluralSegments(n int) string {
	if n == 1 {
		return "1 segment"
	}
	return strconv.Itoa(n) + " segments"
}

const cleanUsage = `/clean removes segments from the model's context window.

  /clean             open the interactive selector (TUI): pick segments to remove
                     and restore excluded ones, with a live reclaim preview
  /clean <handle>…   preview removing the named segments (handles come from /context)
  /clean --apply     confirm the previewed removal
  /clean --undo      restore the most recent removal
  /clean --cancel    discard the preview

Nothing leaves the log — a removal is reversible, and the live thread keeps working.`

// cmdRewind rewinds the conversation to an earlier turn or named mark (AS-037
// /rewind, PRD §7.16): the user picks a checkpoint, sees a preview of exactly
// what leaves the window and a warning listing files modified after that point,
// then confirms. A rewind is an appended exclusion event — history is never
// mutated (D3) — and /rewind --undo restores it exactly.
//
//   - /rewind                  open the checkpoint picker (TUI) / list (CLI)
//   - /rewind <handle>          preview rewinding to that checkpoint and stage it
//   - /rewind --mark "<label>"  drop a named checkpoint at the current point
//   - /rewind --apply           confirm the staged preview, appending the rewind
//   - /rewind --undo            reverse the most recent rewind
//   - /rewind --cancel          discard the staged preview
func (s *chatSession) cmdRewind(_ context.Context, args []string) (command.Output, error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--mark":
			return s.rewindMark(strings.Join(args[1:], " "))
		case "--apply":
			return s.rewindApply()
		case "--undo":
			return s.rewindUndo()
		case "--cancel":
			return s.rewindCancel()
		default:
			return command.Output{}, fmt.Errorf("unknown /rewind flag %q (use --mark, --apply, --undo, or --cancel)", args[0])
		}
	}
	if len(args) == 0 {
		return s.rewindList(), nil
	}
	return s.rewindPreview(args[0])
}

// rewindList renders the session's checkpoints newest-first for both faces: a
// text listing (the scriptable path, with the handle to pass to /rewind) and an
// interactive Picker the TUI opens so a checkpoint can be chosen with the arrow
// keys. Choosing an item re-dispatches /rewind <handle>, which stages the
// preview — the same path a typed handle takes.
func (s *chatSession) rewindList() command.Output {
	s.mu.Lock()
	events := s.sess.Log.Events()
	s.mu.Unlock()

	cps := rewind.Checkpoints(events)
	if len(cps) == 0 {
		return command.Output{Text: "Nothing to rewind to yet — no turns in this session."}
	}

	var b strings.Builder
	b.WriteString("Rewind points (newest first) — /rewind <handle> to preview one:\n\n")
	items := make([]command.PickerItem, 0, len(cps))
	for i := len(cps) - 1; i >= 0; i-- {
		c := cps[i]
		label := rewindLabel(c)
		fmt.Fprintf(&b, "  %-12s %s\n", rewind.ShortAnchor(c.Anchor), label)
		items = append(items, command.PickerItem{Label: label, Value: c.Anchor})
	}
	return command.Output{
		Text:   strings.TrimRight(b.String(), "\n"),
		Picker: &command.Picker{Title: "Rewind to a checkpoint", Items: items},
	}
}

// rewindPreview stages a rewind: it resolves the handle to a checkpoint, builds
// the plan, and stores it pending confirmation. Nothing is appended to the log.
func (s *chatSession) rewindPreview(handle string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.sess.Log.Events()
	target, ok := rewind.Find(events, handle)
	if !ok {
		s.pendingRewind, s.pendingRewindFor = nil, nil
		return command.Output{Text: fmt.Sprintf("No checkpoint matches %q. Run /rewind to list them.", handle)}, nil
	}
	plan := rewind.Preview(events, s.pricing, s.model, time.Now(), target)
	if plan.Empty() {
		s.pendingRewind, s.pendingRewindFor = nil, nil
		return command.Output{Text: rewind.RenderPreview(plan)}, nil
	}
	s.pendingRewind, s.pendingRewindFor = &plan, s.sess
	return command.Output{Text: rewind.RenderPreview(plan)}, nil
}

// rewindApply confirms the staged preview, appending the exclusion event that
// rewinds the conversation. The plan is discarded once applied.
func (s *chatSession) rewindApply() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingRewind == nil {
		return command.Output{Text: "Nothing staged. Run /rewind <handle> to preview a rewind first."}, nil
	}
	if s.pendingRewindFor != s.sess {
		s.pendingRewind, s.pendingRewindFor = nil, nil
		return command.Output{Text: "The staged preview was for a different session and is no longer valid. Run /rewind again."}, nil
	}
	// Recompute against the current log rather than trusting the snapshot taken
	// at preview time: the checkpoint is identified by its stable anchor, and any
	// events appended since the preview must also be named by the rewind, or
	// those newer turns would stay live and the rewind would be incomplete
	// (append-only, so the anchor's index is stable).
	events := s.sess.Log.Events()
	target, ok := rewind.Find(events, s.pendingRewind.Target.Anchor)
	if !ok {
		s.pendingRewind, s.pendingRewindFor = nil, nil
		return command.Output{Text: "The staged checkpoint is no longer in this session. Run /rewind again."}, nil
	}
	plan := rewind.Preview(events, s.pricing, s.model, time.Now(), target)
	event, ok := rewind.Apply(plan)
	if !ok {
		s.pendingRewind, s.pendingRewindFor = nil, nil
		return command.Output{Text: "Nothing to rewind."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record rewind: %w", err)
	}
	s.pendingRewind, s.pendingRewindFor = nil, nil
	text := fmt.Sprintf("Rewound to %s.", rewind.ShortAnchor(plan.Target.Anchor))
	switch net := plan.NetTokens(); {
	case net > 0:
		text += fmt.Sprintf(" Window shrank by ~%d tokens.", net)
	case net < 0:
		text += fmt.Sprintf(" Window grew by ~%d tokens (a later /clean was undone).", -net)
	}
	text += " Restore with /rewind --undo."
	return command.Output{Text: text, ResetView: true}, nil
}

// rewindUndo reverses the most recent rewind by appending a counter-exclusion.
// The log is never rewritten, so the restoration is exact.
func (s *chatSession) rewindUndo() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := rewind.Undo(s.sess.Log.Events())
	if !ok {
		return command.Output{Text: "No /rewind to undo in this session."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record rewind undo: %w", err)
	}
	return command.Output{Text: "Restored the conversation to before the last rewind.", ResetView: true}, nil
}

// rewindMark drops a named manual checkpoint at the current end of the log
// (/rewind --mark "<label>"). The mark is a control event, so it never enters
// the model's window; it only gives the picker a labeled point to rewind to.
func (s *chatSession) rewindMark(label string) (command.Output, error) {
	label = strings.TrimSpace(strings.Trim(strings.TrimSpace(label), `"`))
	if label == "" {
		return command.Output{Text: `Give the mark a label, e.g. /rewind --mark "before refactor".`}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.sess.Log.Append(rewind.Mark(label)); err != nil {
		return command.Output{}, fmt.Errorf("record checkpoint: %w", err)
	}
	return command.Output{Text: fmt.Sprintf("Marked a checkpoint %q. Return to it with /rewind.", label)}, nil
}

// rewindCancel discards a staged preview without touching the log.
func (s *chatSession) rewindCancel() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingRewind == nil {
		return command.Output{Text: "Nothing staged to cancel."}, nil
	}
	s.pendingRewind, s.pendingRewindFor = nil, nil
	return command.Output{Text: "Discarded the staged /rewind preview. Nothing changed."}, nil
}

// rewindLabel formats a checkpoint as a one-line picker/listing row, leading
// with its append time so the turn list reads chronologically (AS-037 picker
// metadata). A zero time (tests, in-memory blocks) is omitted.
func rewindLabel(c rewind.Checkpoint) string {
	stamp := ""
	if !c.Time.IsZero() {
		stamp = c.Time.Local().Format("15:04") + " · "
	}
	if c.Manual {
		return fmt.Sprintf("%s⚑ mark: %s", stamp, c.Label)
	}
	return fmt.Sprintf("%sturn %d: %s", stamp, c.Turn, c.Label)
}

// cmdCompact summarizes the older conversation into one derived block (AS-038
// /compact, PRD §7.16, Appendix A): the fallback for when /clean and /tidy
// aren't enough. The summary is produced by the cheap-tier model and recorded as
// a schema.KindCompaction block whose source blocks are excluded but kept on the
// log (D3) — so the window shrinks, the log keeps everything, and /compact --undo
// restores the exact prior projection.
//
//   - /compact          preview what would be summarized (mutates nothing) and stage it
//   - /compact --apply   confirm: summarize with the cheap tier and append the compaction
//   - /compact --undo    reverse the most recent compaction
//   - /compact --cancel  discard the staged preview
func (s *chatSession) cmdCompact(ctx context.Context, args []string) (command.Output, error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--apply":
			return s.compactApply(ctx)
		case "--undo":
			return s.compactUndo()
		case "--cancel":
			return s.compactCancel()
		default:
			return command.Output{}, fmt.Errorf("unknown /compact flag %q (use --apply, --undo, or --cancel)", args[0])
		}
	}
	return s.compactPreview()
}

// compactPreview stages a compaction: it projects the live window, selects the
// compactable span, and stores the plan pending confirmation. Nothing is
// appended to the log and no model is called.
func (s *chatSession) compactPreview() (command.Output, error) {
	// Snapshot under the lock, then project after releasing it: the projection and
	// composition can be heavy on a large session, and holding s.mu through them
	// would stall the status-line meter and concurrent command dispatch (mirrors
	// cmdContext / rehydrate).
	s.mu.Lock()
	events := s.sess.Log.Events()
	model, pricing, sess := s.model, s.pricing, s.sess
	s.mu.Unlock()

	proj := projection.Project(events, projection.Options{TargetModel: model})
	plan := compact.Preview(proj, pricing, model, time.Now())

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess != sess {
		// A /clear or /resume landed while we were projecting; the plan is for a
		// stale log, so discard it rather than staging it against the wrong session.
		s.pendingCompact, s.pendingCompactFor = nil, nil
		return command.Output{Text: "The session changed while previewing. Run /compact again."}, nil
	}
	if plan.Empty() {
		s.pendingCompact, s.pendingCompactFor = nil, nil
		return command.Output{Text: compact.RenderPreview(plan)}, nil
	}
	s.pendingCompact, s.pendingCompactFor = &plan, s.sess
	return command.Output{Text: compact.RenderPreview(plan)}, nil
}

// compactApply confirms the staged preview: it fires the pre-compact hook, runs
// the cheap-tier summarization, then appends the usage record and the derived
// compaction block. The plan is recomputed against the current log so any turns
// appended since the preview are folded in too (mirrors rewindApply). The plan is
// discarded once applied.
func (s *chatSession) compactApply(ctx context.Context) (command.Output, error) {
	s.mu.Lock()
	if s.pendingCompact == nil {
		s.mu.Unlock()
		return command.Output{Text: "Nothing staged. Run /compact to preview a compaction first."}, nil
	}
	if s.pendingCompactFor != s.sess {
		s.pendingCompact, s.pendingCompactFor = nil, nil
		s.mu.Unlock()
		return command.Output{Text: "The staged preview was for a different session and is no longer valid. Run /compact again."}, nil
	}
	// Recompute against the current log rather than trusting the preview snapshot:
	// newer turns must be summarized too, or they would stay live and the window
	// would not shrink as the preview promised.
	proj := projection.Project(s.sess.Log.Events(), projection.Options{TargetModel: s.model})
	plan := compact.Preview(proj, s.pricing, s.model, time.Now())
	log, sessID := s.sess.Log, s.sess.ID
	vendor, model := s.cheapModel()
	prov := s.providers[vendor]
	s.mu.Unlock()

	if plan.Empty() {
		s.clearPendingCompact()
		return command.Output{Text: "Nothing to compact."}, nil
	}
	if prov == nil {
		return command.Output{}, fmt.Errorf("no provider configured for vendor %q", vendor)
	}

	// Fire the pre-compact lifecycle hook (AS-035) before any summarization: a
	// hook may veto the compaction (e.g. a policy that forbids lossy edits).
	out := fireLifecycle(ctx, s.hooks, log, hook.Payload{
		Event:   hook.PreCompact,
		Session: sessID,
		Reason:  "user", // /compact is user-invoked, not auto-triggered
	})
	if out.Blocked {
		s.clearPendingCompact()
		return command.Output{Text: "Compaction blocked by a pre-compact hook: " + out.Reason}, nil
	}

	summary, tokens, stopReason, err := summarize(ctx, prov, model, plan.Sources)
	if err != nil {
		return command.Output{}, fmt.Errorf("summarize for /compact: %w", err)
	}
	block, ok := compact.Build(plan, summary)
	if !ok {
		s.clearPendingCompact()
		return command.Output{Text: "The summarizer returned nothing; the conversation was left unchanged."}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Record the summarization's token usage so /cost itemizes it on the cheap
	// tier (AS-038 AC4); a nil-usage surface simply records nothing.
	if tokens != nil {
		if _, err := s.sess.Log.Append(eventlog.NewUsage(compact.Producer, vendor, model, stopReason, tokens, nil)); err != nil {
			return command.Output{}, fmt.Errorf("record compaction usage: %w", err)
		}
	}
	if _, err := s.sess.Log.Append(block); err != nil {
		return command.Output{}, fmt.Errorf("record compaction: %w", err)
	}
	s.pendingCompact, s.pendingCompactFor = nil, nil
	return command.Output{
		Text: fmt.Sprintf("Compacted %s into one summary, reclaiming ~%d tokens. Restore with /compact --undo.",
			pluralBlocks(len(plan.SourceIDs)), plan.Tokens),
		ResetView: true,
	}, nil
}

// compactUndo reverses the most recent compaction by appending a
// counter-exclusion. The log is never rewritten, so the restoration is exact.
func (s *chatSession) compactUndo() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, removed, ok := compact.Undo(s.sess.Log.Events())
	if !ok {
		return command.Output{Text: "No /compact to undo in this session."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record compaction undo: %w", err)
	}
	return command.Output{
		Text:      fmt.Sprintf("Restored %s to the window.", pluralBlocks(removed)),
		ResetView: true,
	}, nil
}

// compactCancel discards a staged preview without touching the log.
func (s *chatSession) compactCancel() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingCompact == nil {
		return command.Output{Text: "Nothing staged to cancel."}, nil
	}
	s.pendingCompact, s.pendingCompactFor = nil, nil
	return command.Output{Text: "Discarded the staged /compact preview. Nothing changed."}, nil
}

// clearPendingCompact drops a staged plan under the lock. It is used on the
// abort paths of compactApply, which release the lock to run the model call.
func (s *chatSession) clearPendingCompact() {
	s.mu.Lock()
	s.pendingCompact, s.pendingCompactFor = nil, nil
	s.mu.Unlock()
}

// cheapModel resolves the cheap-tier model to summarize with, keeping the active
// vendor (so its provider is already configured) and dropping to that vendor's
// cheapest family (AS-038 AC4). An unknown vendor falls back to the active model
// rather than guessing an id the provider would reject.
func (s *chatSession) cheapModel() (vendor, model string) {
	switch s.provName {
	case "anthropic":
		return s.provName, "claude-haiku-4-5"
	case "openai":
		return s.provName, "gpt-4o-mini"
	default:
		return s.provName, s.model
	}
}

// pluralBlocks labels a block count for the compact confirm/undo lines.
func pluralBlocks(n int) string {
	if n == 1 {
		return "1 block"
	}
	return strconv.Itoa(n) + " blocks"
}

// summarize runs a one-shot, non-streaming-consumed summarization turn against
// prov with model, asking it to condense the rendered transcript of the
// compactable blocks. It returns the summary text, the turn's reported token
// usage (nil when the surface reports none), and the turn's stop reason (so the
// recorded usage event reflects max_tokens/refusal rather than asserting
// end_turn). Caching is disabled — this prefix recurs only once.
func summarize(ctx context.Context, prov provider.Provider, model string, sources []schema.Block) (string, *schema.Tokens, string, error) {
	req := provider.Request{
		Model: model,
		Context: []schema.Block{
			{
				ID:   schema.NewID(),
				Kind: schema.KindText,
				Role: schema.RoleSystem,
				Text: &schema.TextBody{Text: summarizeInstruction, Subtype: schema.TextSubtypeNormal},
			},
			{
				ID:   schema.NewID(),
				Kind: schema.KindText,
				Role: schema.RoleUser,
				Text: &schema.TextBody{Text: "Transcript to summarize:\n\n" + compact.Transcript(sources), Subtype: schema.TextSubtypeNormal},
			},
		},
		Params: provider.SamplingParams{MaxTokens: 1024},
		Cache:  provider.CacheHints{Disabled: true},
	}
	stream, err := prov.Stream(ctx, req)
	if err != nil {
		return "", nil, "", err
	}
	defer stream.Close() //nolint:errcheck // best-effort once drained

	var text strings.Builder
	var usage *schema.Tokens
	var stopReason string
	for stream.Next() {
		ev := stream.Event()
		switch ev.Type {
		case provider.EventTextDelta:
			text.WriteString(ev.TextDelta)
		case provider.EventUsage:
			usage = mergeTokens(usage, ev.Usage)
		case provider.EventTurnStop:
			stopReason = ev.StopReason
		}
	}
	if err := stream.Err(); err != nil {
		return "", nil, "", err
	}
	return text.String(), usage, stopReason, nil
}

// summarizeInstruction is the cheap-tier summarizer's system prompt.
const summarizeInstruction = "You compress conversation history. Summarize the transcript below into a concise " +
	"summary that preserves decisions made, facts established, file paths, identifiers, and any open or pending tasks, " +
	"so the assistant can keep working without the full history. Write the summary as plain prose. Output only the summary."

// mergeTokens sums two optional usage breakdowns field-wise, treating a nil
// field as unreported (not zero), so the disjoint usage events a turn reports
// (input at the start, output at the end) accumulate. It mirrors the loop's own
// accumulation; a nil result means neither side reported anything.
func mergeTokens(dst, src *schema.Tokens) *schema.Tokens {
	if src == nil {
		return dst
	}
	if dst == nil {
		dst = &schema.Tokens{}
	}
	dst.Input = addInt(dst.Input, src.Input)
	dst.Output = addInt(dst.Output, src.Output)
	dst.CacheRead = addInt(dst.CacheRead, src.CacheRead)
	dst.CacheWrite = addInt(dst.CacheWrite, src.CacheWrite)
	dst.Reasoning = addInt(dst.Reasoning, src.Reasoning)
	return dst
}

// addInt sums two optional counts, preserving "nil means unreported": nil + nil
// stays nil, and a reported value (even zero) makes the result present. When one
// side is nil the other pointer is returned as-is — no caller mutates the
// pointed-to int, so sharing it avoids a needless heap allocation.
func addInt(a, b *int) *int {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	sum := *a + *b
	return &sum
}

// cmdClear ends the current session and starts a fresh one (AS-023 /clear). The
// previous session stays on disk and resumable (append-only ethos, D3); only the
// active log is swapped, so the next turn projects a clean, empty context.
func (s *chatSession) cmdClear(context.Context, []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.store.Create("")
	if err != nil {
		return command.Output{}, fmt.Errorf("start new session: %w", err)
	}
	// Seed the fresh session with the project's memory files, so a cleared
	// session starts from the same standing context a freshly launched one does
	// (AS-032). A discovery error is surfaced rather than silently dropping it.
	if err := seedMemory(s.wd, fresh); err != nil {
		_ = fresh.Log.Close()
		return command.Output{}, err
	}
	if err := seedSkills(fresh, s.skills); err != nil {
		_ = fresh.Log.Close()
		return command.Output{}, err
	}
	eng, err := s.buildEngine(fresh, s.provName, s.model)
	if err != nil {
		_ = fresh.Log.Close() // built nothing; don't leak the fresh log's handle
		return command.Output{}, err
	}
	prev := s.sess
	s.sess, s.engine = fresh, eng
	// Safe to close the previous log: the busy guard means no turn is writing it
	// when a swap runs, so its file descriptor isn't leaked across /clear.
	_ = prev.Log.Close()
	return command.Output{Text: "Started a new session (" + shortID(fresh.ID) + "). The previous one is still in /resume.", ResetView: true}, nil
}

// cmdModel lists the configured models, or switches the active provider/model
// mid-session (AS-023 /model). The switch is recorded on the log as a
// model-switch event so cost attribution and the transcript stay accurate, and a
// resumed session can recover the model it was last using. The vendor is
// resolved from the pricing table, so switching across providers (Anthropic ↔
// OpenAI) works through the normalized block schema (D4).
func (s *chatSession) cmdModel(_ context.Context, args []string) (command.Output, error) {
	if len(args) == 0 {
		return command.Output{Text: s.modelListing()}, nil
	}
	target := strings.TrimSpace(args[0])
	// The pricing table holds wildcard family patterns (e.g. "gpt-4o*"); a bare
	// pattern would Lookup as an exact match and become an invalid active model.
	// Require a concrete id so the next turn issues a real model.
	if strings.Contains(target, "*") {
		return command.Output{}, fmt.Errorf("model %q is a family pattern; pass a concrete model id (e.g. gpt-4o)", target)
	}
	rate, ok := s.pricing.Lookup(target)
	if !ok || rate.Vendor == "" {
		return command.Output{}, fmt.Errorf("unknown model %q: not in the pricing table, so its provider can't be resolved", target)
	}
	if _, ok := s.providers[rate.Vendor]; !ok {
		return command.Output{}, fmt.Errorf("no provider configured for vendor %q (model %q)", rate.Vendor, target)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if target == s.model && rate.Vendor == s.provName {
		return command.Output{Text: "Already on " + s.provName + " · " + s.model + "."}, nil
	}
	// Build the new engine before mutating any state or appending the switch
	// event, so a build failure leaves the session on its current model.
	eng, err := s.buildEngine(s.sess, rate.Vendor, target)
	if err != nil {
		return command.Output{}, err
	}
	if _, err := s.sess.Log.Append(eventlog.NewModelSwitch("/model", rate.Vendor, target)); err != nil {
		return command.Output{}, fmt.Errorf("record model switch: %w", err)
	}
	prev := s.provName + " · " + s.model
	s.provName, s.model, s.engine = rate.Vendor, target, eng
	return command.Output{Text: "Model switched: " + prev + " → " + s.provName + " · " + s.model + ". The next turn uses it."}, nil
}

// modelListing renders the configured model families for the vendors that have a
// provider wired, marking the active one. Entries are pricing-table patterns
// (e.g. "claude-opus-4-*"); pass a concrete id to /model to switch.
func (s *chatSession) modelListing() string {
	s.mu.Lock()
	current := s.provName + " · " + s.model
	s.mu.Unlock()

	type row struct{ vendor, model string }
	var rows []row
	for _, r := range s.pricing.Models() {
		if _, ok := s.providers[r.Vendor]; !ok {
			continue
		}
		rows = append(rows, row{vendor: r.Vendor, model: r.Model})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].vendor != rows[j].vendor {
			return rows[i].vendor < rows[j].vendor
		}
		return rows[i].model < rows[j].model
	})

	var b strings.Builder
	fmt.Fprintf(&b, "Active model: %s\n\n", current)
	b.WriteString("Configured model families (pass a concrete id, e.g. /model claude-sonnet-4-6):\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-10s %s\n", r.vendor, r.model)
	}
	return strings.TrimRight(b.String(), "\n")
}

// cmdResume lists this project's recent sessions, or loads one by ID (AS-023
// /resume). Loading swaps the active log to the stored one, so the next turn
// projects exactly its last live state; the active model is restored to the one
// the session last used so the window and cost meter match.
func (s *chatSession) cmdResume(_ context.Context, args []string) (command.Output, error) {
	if len(args) == 0 {
		return s.resumeList(), nil
	}
	id := strings.TrimSpace(args[0])
	opened, err := s.store.Open(id)
	if err != nil {
		return command.Output{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Adopt the session's last-used model only when its provider is configured,
	// so the model and provider never disagree (a model with no provider would
	// fail at turn time). Otherwise keep the current provider/model.
	provName, model := s.provName, s.model
	if m := lastModel(opened.Log.Events()); m != "" {
		if r, ok := s.pricing.Lookup(m); ok && r.Vendor != "" {
			if _, ok := s.providers[r.Vendor]; ok {
				provName, model = r.Vendor, m
			}
		}
	}
	eng, err := s.buildEngine(opened, provName, model)
	if err != nil {
		_ = opened.Log.Close() // won't switch to it; don't leak the opened log's handle
		return command.Output{}, err
	}
	prev := s.sess
	s.sess, s.provName, s.model, s.engine = opened, provName, model, eng
	// Close the previously-active log; the busy guard means no turn is writing it.
	_ = prev.Log.Close()
	return command.Output{
		Text:      "Resumed session " + shortID(opened.ID) + " on " + s.provName + " · " + s.model + ".",
		ResetView: true,
	}, nil
}

// resumeList renders the project's sessions newest-first for both faces: a text
// listing (the scriptable `smith session list`, with the full ID to pass to
// /resume) and an interactive Picker the TUI opens so a session can be chosen
// with the arrow keys and Enter instead of copy-pasting an ID (AS-064). Both are
// built from the same per-session detail line, so the listing and the picker can
// never disagree. Cost is derived from each session's log through the same
// accounting source as /cost.
func (s *chatSession) resumeList() command.Output {
	summaries, err := s.store.List()
	if err != nil {
		return command.Output{Text: "Couldn't list sessions: " + err.Error()}
	}
	if len(summaries) == 0 {
		return command.Output{Text: "No sessions yet for this project."}
	}

	s.mu.Lock()
	currentID := s.sess.ID
	s.mu.Unlock()

	var b strings.Builder
	b.WriteString("Sessions for this project (newest first) — /resume <id> to load one:\n\n")
	now := time.Now()
	items := make([]command.PickerItem, 0, len(summaries))
	for _, sum := range summaries {
		detail := s.sessionDetail(sum, now)
		marker := "  "
		label := shortID(sum.ID) + " · " + detail
		if sum.ID == currentID {
			marker = "▸ "
			label += " (current)"
		}
		fmt.Fprintf(&b, "%s%s\n    %s\n", marker, sum.ID, detail)
		items = append(items, command.PickerItem{Label: label, Value: sum.ID})
	}
	return command.Output{
		Text:   strings.TrimRight(b.String(), "\n"),
		Picker: &command.Picker{Title: "Resume a session", Items: items},
	}
}

// sessionDetail formats a session's one-line summary — title, event count, age,
// cost, size, and the models used — shared by the /resume text listing and the
// interactive picker so the two surfaces describe a session identically.
func (s *chatSession) sessionDetail(sum session.Summary, now time.Time) string {
	models := strings.Join(sum.Models, ", ")
	if models == "" {
		models = "—"
	}
	title := sum.Title
	if title == "" {
		title = "(untitled)"
	}
	return fmt.Sprintf("%s · %d events · %s · %s · %s · %s",
		title, sum.EventCount, humanAge(now.Sub(sum.UpdatedAt)), s.sessionCostLabel(sum.ID), humanBytes(sum.SizeBytes), models)
}

// rehydrate returns the active session's projected live blocks for the face to
// rebuild its visible transcript after a /clear or /resume (AS-064). It is pure
// projection at the active model — no model calls — so a resumed session shows
// its prior turns exactly as the window holds them, and a freshly cleared
// session yields no blocks (an empty transcript).
func (s *chatSession) rehydrate() []schema.Block {
	// Snapshot the log and model under the lock, then project after releasing it:
	// Events() already returns a copy, so the projection (potentially heavy on a
	// large session) doesn't block other readers of the controller state.
	s.mu.Lock()
	events := s.sess.Log.Events()
	model := s.model
	s.mu.Unlock()
	return projection.Project(events, projection.Options{TargetModel: model}).Live()
}

// sessionCostLabel computes a session's accrued cost for the /resume listing
// using the same accounting source as /cost. It opens a throwaway read handle on
// the session log and closes it, so the listing never leaks descriptors. An
// unreadable session or an unpriced turn degrades to "<sym>?" rather than a
// misleadingly exact figure.
func (s *chatSession) sessionCostLabel(id string) string {
	opened, err := s.store.Open(id)
	if err != nil {
		return "—"
	}
	summary := cost.Summarize(opened.Log.Events(), s.pricing)
	_ = opened.Log.Close()
	sym := cost.Symbol(summary.Currency)
	if !summary.AllPriced {
		return sym + "?"
	}
	return sym + strconv.FormatFloat(summary.TotalUSD, 'f', 4, 64)
}

// lastModel returns the most recent model recorded on the log (by the latest
// block carrying a provider model — a turn or a /model switch), or "" when the
// log records none. It lets /resume restore the model a session last used.
func lastModel(events []schema.Block) string {
	for i := len(events) - 1; i >= 0; i-- {
		if p := events[i].Provider; p != nil && p.Model != "" {
			return p.Model
		}
	}
	return ""
}

// humanAge formats a duration as a compact age for the session list.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// humanBytes formats a byte size compactly for the session list.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
