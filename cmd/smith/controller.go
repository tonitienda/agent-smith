package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/clean"
	"github.com/tonitienda/agent-smith/internal/codingskills"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/compact"
	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/goal"
	"github.com/tonitienda/agent-smith/internal/hook"
	"github.com/tonitienda/agent-smith/internal/initscaffold"
	"github.com/tonitienda/agent-smith/internal/insights"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/memory"
	"github.com/tonitienda/agent-smith/internal/mode"
	"github.com/tonitienda/agent-smith/internal/permission"
	"github.com/tonitienda/agent-smith/internal/personality"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/rewind"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/skillrollup"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/internal/tidy"
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
	// codingPack is the bundled Coding Mode process skill pack (AS-074),
	// auto-enabled per phase while the mode is active. It is loaded once, lazily,
	// under s.mu (the command handlers that trigger injection already hold the
	// lock); codingPackDone guards that one-time load so a parse error is not
	// retried every transition. A bundled-pack parse failure is non-fatal — the
	// mode is a soft advisor (D-CODE-2), so injection is simply skipped.
	codingPack     []skill.Skill
	codingPackDone bool
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

	// pers is the Matrix personality layer (AS-053): it renders the themed status
	// line and role names for the interactive chrome and backs /serious. It is
	// chrome-only and never touches turn behavior or output. nil leaves the face
	// on its plain defaults.
	pers *personality.Personality

	// subagents is the sub-agent registry (AS-044/AS-107): built once at startup
	// with the built-in system sub-agents registered and the `subagents.<name>`
	// config overlay applied. buildEngine constructs a fresh per-session Runner
	// over it; nil leaves the loop with no Runner installed (it then costs nothing).
	subagents *subagent.Registry
	// insights is the findings store the controller owns for the session's lifetime
	// (the seam /insights, AS-045, reads). It is reused across engine rebuilds
	// (/model, /clear, /resume) so findings accumulate rather than resetting with a
	// new engine; each Runner the controller builds records into this same store.
	insights subagent.Store
	// rollup is the durable, cross-session findings store (AS-050) the /skills
	// report reads. It is the same object as insights when a session store is
	// present (a *skillrollup.Store satisfies subagent.Store), captured here under
	// its concrete type so /skills can call Rollup/Resolve; nil when the face wired
	// a plain in-memory store, which leaves /skills on its session-only view.
	rollup *skillrollup.Store

	// router is the model routing/tiering policy (AS-042): tier-declaring work
	// (/compact summarization) resolves its concrete model through it instead of
	// hardcoding ids. Defaults to routing.Default() so a face that never calls
	// setRouter keeps the previous hardcoded cheap-tier behavior. `/route <feature>
	// <tier>` / `/route <tier> <vendor> <model>` layer transient per-session
	// overrides on top (AS-110) — copied, never mutating baseRouter.
	router routing.Policy
	// baseRouter is the durable config-derived policy (AS-110): the value /route
	// overrides layer onto and that a session swap (/clear, /resume) resets router
	// back to, so per-session overrides never outlive the session that set them and
	// the config policy stays untouched.
	baseRouter routing.Policy

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

	// pendingTidy holds the previewed /tidy dedup plan awaiting confirmation
	// (/tidy --apply) or discard (/tidy --cancel), keyed to the session it was
	// previewed against so a /clear or /resume between preview and apply
	// invalidates it rather than deduping the wrong log (mirrors pendingClean).
	pendingTidy    *tidy.Plan
	pendingTidyFor *session.Session

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

	// pendingInit holds the previewed /init scaffold awaiting confirmation
	// (/init --apply) or discard (/init --cancel), keyed to the session it was
	// previewed against so a /clear or /resume between preview and apply
	// invalidates it rather than writing files staged for a different session
	// (mirrors pendingClean). The scan reads the filesystem, not the log, but the
	// keying keeps the confirm/cancel lifecycle identical across commands.
	pendingInit    *initscaffold.Plan
	pendingInitFor *session.Session

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

	// mode chrome memo: the Coding Mode name, pinned phase tracker, and richer
	// panel (AS-073) the status line shows. Like the goal memo, Meta() runs on
	// every token delta, so the mode projection (Current scans the log) is cached
	// and only re-derived when the active log or its length changes.
	modeLog     *eventlog.Log
	modeLen     int
	modeName    string
	modeTracker string
	modePanel   string
}

// newChatSession builds the controller over an already-opened session, wiring
// the default Anthropic + OpenAI providers and the model for the first turn. The
// engine is not built yet: the caller sets the observer (from the TUI) and calls
// start so turn progress is wired before the first turn runs.
func newChatSession(store *session.Store, tools *tool.Registry, pricing *cost.Table, providers map[string]provider.Provider, sess *session.Session, provName, model, wd string, skills []skill.Skill, hooks *hook.Set) *chatSession {
	return &chatSession{
		store:      store,
		tools:      tools,
		pricing:    pricing,
		providers:  providers,
		sess:       sess,
		provName:   provName,
		model:      model,
		project:    filepath.Base(wd),
		wd:         wd,
		skills:     skills,
		hooks:      hooks,
		router:     routing.Default(),
		baseRouter: routing.Default(),
	}
}

// setRouter installs the model routing/tiering policy (AS-042), read from layered
// config at startup via routing.ConfigFrom (AS-093). A face that does not call it
// keeps routing.Default() — the previous hardcoded cheap-tier behavior. It seeds
// both the effective router and the baseRouter a session swap resets to (AS-110).
func (s *chatSession) setRouter(p routing.Policy) { s.router, s.baseRouter = p, p }

// setBudgetDefaults records the configured budget ceiling default and warning
// fraction (AS-041), read from layered config at startup. A non-positive default
// leaves new sessions budget-free until /budget sets one; a warn fraction outside
// (0,1) falls back to the budget package default downstream.
func (s *chatSession) setBudgetDefaults(defaultUSD, warnFraction float64, haltUnpriced bool) {
	s.budgetDefaultUSD = defaultUSD
	s.budgetWarnFraction = warnFraction
	s.budgetHaltUnpriced = haltUnpriced
}

// setPersonality installs the Matrix personality layer (AS-053), built from the
// layered config for the interactive face. A nil personality leaves the chrome
// on its plain defaults.
func (s *chatSession) setPersonality(p *personality.Personality) {
	s.pers = p
}

// setSubAgents installs the sub-agent registry and the insights store the
// controller owns (AS-107). buildEngine then constructs a per-session Runner over
// them and drives it via loop.WithSubAgents, so findings accumulate in the store
// across engine rebuilds. A nil registry leaves sub-agents disabled (the loop's
// Runner is then never installed), so a face that opts out pays nothing.
func (s *chatSession) setSubAgents(reg *subagent.Registry, store subagent.Store) {
	s.subagents = reg
	s.insights = store
	// Capture the concrete rollup store when one was wired, so /skills can read the
	// cross-session aggregation and resolve applied remedies (AS-050).
	if r, ok := store.(*skillrollup.Store); ok {
		s.rollup = r
	}
}

// workingLine yields the status-line text shown while a turn runs: the themed
// line when the personality is active, the plain default otherwise. It is the
// only seam the theme reaches the TUI through (the face never imports the flavor
// package). A nil personality returns "" so the face keeps its plain default.
func (s *chatSession) workingLine() string {
	if s.pers == nil {
		return ""
	}
	return s.pers.StatusLine()
}

// setAutoCompact records the auto-compaction config (AS-085), read once from
// layered config at startup via compact.ConfigFrom (AS-093), which owns the
// `compact.*` paths and normalizes the threshold into (0,1). enabled off
// (default) is a no-op at turn time.
func (s *chatSession) setAutoCompact(enabled bool, threshold float64) {
	s.autoCompact = enabled
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
	// Sub-agent lifecycle (AS-107): a fresh Runner per engine, keyed to this
	// session and recording into the controller-owned store, so the loop drives the
	// built-in passive analyzers (AS-048) and their findings outlive an engine
	// rebuild. No-op cost when nothing is enabled (the Runner's fan-out is empty).
	if s.subagents != nil {
		runner := subagent.NewRunner(s.subagents, s.insights, sess.ID)
		opts = append(opts, loop.WithSubAgents(runner))
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
	goal := s.currentGoal()
	name, tracker, panel := s.currentModeChrome(goal)
	return tui.Meta{
		Provider:     s.provName,
		Model:        s.model,
		Session:      shortID(s.sess.ID),
		Project:      s.project,
		Goal:         goal,
		Mode:         name,
		PhaseTracker: tracker,
		ModePanel:    panel,
	}
}

// currentModeChrome returns the active Coding Mode name and the pre-rendered
// status-line tracker and inspect panel (AS-073), or empties when no mode is
// active. The render is memoized on the active log and its length so the
// per-delta status refresh (refreshMeter → Meta) stays O(1) like the goal and
// meter; it only re-derives when the log grows or is swapped. The face displays
// these strings as chrome, so the mode package stays out of the TUI's imports.
// Callers must hold s.mu. goal is threaded in (not re-projected) so the cached
// panel reflects the current objective.
func (s *chatSession) currentModeChrome(goal string) (name, tracker, panel string) {
	log := s.sess.Log
	if log == s.modeLog && log.Len() == s.modeLen {
		return s.modeName, s.modeTracker, s.modePanel
	}
	events := log.Events()
	name, tracker, panel = "", "", ""
	if cur, ok := mode.Current(events); ok {
		method := resolveMethod(events)
		name = cur.Mode
		tracker = mode.Tracker(method.Phases, cur.Phase)
		panel = mode.Panel(events, method.Phases, goal, method.Rules)
	}
	s.modeLog, s.modeLen = log, log.Len()
	s.modeName, s.modeTracker, s.modePanel = name, tracker, panel
	return name, tracker, panel
}

// resolveMethod layers any project method overrides (AS-075) from the session's
// memory blocks over the baked-in house method (D-CODE-5). The overrides ride on
// the log as ordinary memory blocks (AS-032), so the resolution stays log-derived
// (D3) and respects precedence: memory blocks are appended lowest-precedence
// first, which is the order ResolveMethod folds them in.
func resolveMethod(events []schema.Block) mode.Method {
	var memos []string
	for _, b := range events {
		if _, ok := memory.Source(b); ok && b.Text != nil {
			memos = append(memos, b.Text.Text)
		}
	}
	return mode.ResolveMethod(mode.DefaultPhases(), memos)
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

// cmdFeature enters Coding Mode with a feature prompt (AS-072 /feature): it sets
// the session goal to the prompt (AS-040) and enters the mode at the first phase.
// Coding Mode is the thin lifecycle shell (coding-mode.prd.md D-CODE-1) — entry,
// phase state, and exit are all events on the log (D3), and the mode is a soft
// advisor that never gates work (D-CODE-2).
//
//   - /feature "<prompt>"  set the goal, enter coding mode, start at "think"
//
// Only one feature/mode runs per session (V1): with one already active, /feature
// asks the user to exit first rather than silently replacing it.
func (s *chatSession) cmdFeature(_ context.Context, args []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.sess.Log.Events()

	if cur, ok := mode.Current(events); ok {
		return command.Output{Text: fmt.Sprintf(
			"Already in %s mode (phase: %s). Exit with /mode off before starting a new feature.",
			cur.Mode, cur.Phase)}, nil
	}

	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		return command.Output{}, fmt.Errorf(`/feature needs a prompt, e.g. /feature "add OAuth login"`)
	}

	// Set the goal first (retiring any active goal so exactly one stays live, as
	// /goal does); the mode then anchors to it.
	if g, ok := goal.Current(events); ok {
		if _, err := s.sess.Log.Append(goal.Retire(g.BlockID)); err != nil {
			return command.Output{}, fmt.Errorf("retire previous goal: %w", err)
		}
	}
	if _, err := s.sess.Log.Append(goal.Set(prompt)); err != nil {
		return command.Output{}, fmt.Errorf("record goal: %w", err)
	}
	if err := s.enterMode(); err != nil {
		return command.Output{}, err
	}
	events = s.sess.Log.Events()
	return command.Output{Text: fmt.Sprintf("Entered coding mode · goal: %s\n%s", prompt, mode.Render(events, resolveMethod(events).Phases))}, nil
}

// cmdMode enters or exits Coding Mode, or shows its status (AS-072 /mode). The
// mode lives on the event log (D3): entering and exiting are appended events and
// phase history survives the exit.
//
//   - /mode            show the active mode and its phase tracker
//   - /mode coding     enter coding mode (without setting a goal)
//   - /mode off        exit the mode (phase history stays on the log)
func (s *chatSession) cmdMode(_ context.Context, args []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.sess.Log.Events()

	if len(args) == 0 {
		return command.Output{Text: mode.Render(events, resolveMethod(events).Phases)}, nil
	}

	switch arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " "))); arg {
	case "off", "exit", "none":
		cur, ok := mode.Current(events)
		if !ok {
			return command.Output{Text: "No coding mode active."}, nil
		}
		if _, err := s.sess.Log.Append(mode.Exit(cur.InstanceID)); err != nil {
			return command.Output{}, fmt.Errorf("exit coding mode: %w", err)
		}
		return command.Output{Text: "Exited coding mode. Phase history stays on the log."}, nil
	case mode.Coding:
		if cur, ok := mode.Current(events); ok {
			return command.Output{Text: fmt.Sprintf("Already in %s mode (phase: %s).", cur.Mode, cur.Phase)}, nil
		}
		if err := s.enterMode(); err != nil {
			return command.Output{}, err
		}
		events = s.sess.Log.Events()
		return command.Output{Text: "Entered coding mode.\n" + mode.Render(events, resolveMethod(events).Phases)}, nil
	default:
		return command.Output{}, fmt.Errorf("unknown mode %q; use `coding` or `off`", arg)
	}
}

// cmdPhase advances, rewinds, or jumps to a Coding Mode phase (AS-072 /phase).
// Every transition is an appended event (D3) and the user can move to any phase
// at any time — nothing is gated (D-CODE-2).
//
//   - /phase           show the current phase and tracker
//   - /phase next      advance to the next phase
//   - /phase back      step to the previous phase
//   - /phase <name>    jump to a named phase
func (s *chatSession) cmdPhase(_ context.Context, args []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.sess.Log.Events()

	cur, ok := mode.Current(events)
	if !ok {
		return command.Output{}, fmt.Errorf("no coding mode active; start one with /feature or /mode coding")
	}
	phases := resolveMethod(events).Phases
	if len(args) == 0 {
		return command.Output{Text: mode.Render(events, phases)}, nil
	}

	arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	var target string
	switch arg {
	case "next":
		t, ok := mode.NextPhase(phases, cur.Phase)
		if !ok {
			return command.Output{Text: fmt.Sprintf("Already at the last phase (%s).", cur.Phase)}, nil
		}
		target = t
	case "back", "prev", "previous":
		t, ok := mode.PrevPhase(phases, cur.Phase)
		if !ok {
			return command.Output{Text: fmt.Sprintf("Already at the first phase (%s).", cur.Phase)}, nil
		}
		target = t
	default:
		t, ok := mode.CanonicalPhase(phases, arg)
		if !ok {
			return command.Output{}, fmt.Errorf("unknown phase %q; phases: %s", arg, strings.Join(phases, ", "))
		}
		target = t
	}

	if _, err := s.sess.Log.Append(mode.SetPhase(cur.InstanceID, target)); err != nil {
		return command.Output{}, fmt.Errorf("record phase change: %w", err)
	}
	// Auto-load the target phase's process skills (AS-074); a no-op for phases
	// that declare none.
	if err := s.injectPhaseSkills(); err != nil {
		return command.Output{}, err
	}
	return command.Output{Text: fmt.Sprintf("Phase → %s\n%s", target, mode.Tracker(phases, target))}, nil
}

// enterMode appends the coding-mode entry events (mode_enter + initial
// phase-change) and auto-loads the first phase's process skills. Callers hold
// s.mu and have already checked no mode is active.
func (s *chatSession) enterMode() error {
	phases := resolveMethod(s.sess.Log.Events()).Phases
	for _, b := range mode.Enter(mode.Coding, phases) {
		if _, err := s.sess.Log.Append(b); err != nil {
			return fmt.Errorf("enter coding mode: %w", err)
		}
	}
	return s.injectPhaseSkills()
}

// phaseSkillProducer attributes the auto-loaded process-skill context blocks
// (AS-074) so /context can show where the guidance came from and so injection
// can dedupe against what the log already carries. extPhaseSkillPhase tags an
// injected block with the phase it was loaded for, so re-entering a phase does
// not re-inject, the trail is auditable on the log, and the projection engine can
// scope the block to its phase (AS-114). Both are defined in eventlog so the
// projection engine shares the same vocabulary.
const (
	phaseSkillProducer = eventlog.PhaseSkillProducer
	extPhaseSkillPhase = eventlog.ExtPhaseSkillPhase
)

// injectPhaseSkills auto-loads the active phase's process skills (AS-074) into
// context, the way the skill tool would but without waiting for the model to ask
// (D-CODE-5.2/-6: bundled, auto-enabled per phase). It is a no-op when no coding
// mode is active, so the pack adds zero cost outside the mode. Each skill body is
// appended once per (instance, phase): re-entering a phase, or calling this after
// a transition that did not change phase, injects nothing new. A project/user
// skill of the same name shadows the bundled one (AS-075); an override with an
// empty body disables that skill for the phase. Callers hold s.mu.
func (s *chatSession) injectPhaseSkills() error {
	events := s.sess.Log.Events()
	cur, ok := mode.Current(events)
	if !ok {
		return nil
	}
	names := mode.PhaseSkills(cur.Phase)
	if len(names) == 0 {
		return nil
	}
	loaded := s.loadedPhaseSkills(events, cur.InstanceID, cur.Phase)
	phaseJSON, err := json.Marshal(cur.Phase)
	if err != nil {
		return fmt.Errorf("marshal phase: %w", err)
	}
	for _, name := range names {
		if loaded[name] {
			continue
		}
		body, ok := s.resolvePhaseSkill(name)
		if !ok || strings.TrimSpace(body) == "" {
			// Unknown skill or an override that disables it: nothing to load.
			continue
		}
		b := schema.Block{
			ID:          schema.NewID(),
			Kind:        schema.KindText,
			Role:        schema.RoleSystem,
			Text:        &schema.TextBody{Text: body},
			Provenance:  &schema.Provenance{Producer: phaseSkillProducer, DerivedFrom: []string{cur.InstanceID}},
			Attribution: &schema.Attribution{Skill: name},
			Ext:         map[string]json.RawMessage{extPhaseSkillPhase: phaseJSON},
		}
		if _, err := s.sess.Log.Append(b); err != nil {
			return fmt.Errorf("auto-load phase skill %q: %w", name, err)
		}
	}
	return nil
}

// loadedPhaseSkills returns the set of process-skill names already auto-loaded
// for the given mode instance and phase, so injection stays idempotent across
// repeated phase entries.
func (s *chatSession) loadedPhaseSkills(events []schema.Block, instanceID, phase string) map[string]bool {
	loaded := map[string]bool{}
	for _, b := range events {
		if b.Provenance == nil || b.Provenance.Producer != phaseSkillProducer || b.Attribution == nil {
			continue
		}
		if !derivedFrom(b, instanceID) {
			continue
		}
		var p string
		if raw, present := b.Ext[extPhaseSkillPhase]; present {
			if err := json.Unmarshal(raw, &p); err == nil && strings.EqualFold(p, phase) {
				loaded[b.Attribution.Skill] = true
			}
		}
	}
	return loaded
}

// resolvePhaseSkill returns the instruction body for a named process skill,
// preferring a user/project skill of the same name (AS-075 override) over the
// bundled pack. ok is false when no skill of that name exists anywhere.
func (s *chatSession) resolvePhaseSkill(name string) (body string, ok bool) {
	for _, sk := range s.skills { // project/user override shadows the bundled pack
		if sk.Name == name {
			return sk.Body, true
		}
	}
	for _, sk := range s.bundledPack() {
		if sk.Name == name {
			return sk.Body, true
		}
	}
	return "", false
}

// bundledPack lazily loads and memoizes the bundled process skill pack (AS-074).
// A parse error leaves the pack empty rather than failing the session — the mode
// is a soft advisor, and the embedded pack is covered by codingskills tests.
// Callers hold s.mu.
func (s *chatSession) bundledPack() []skill.Skill {
	if !s.codingPackDone {
		s.codingPackDone = true
		if pack, err := codingskills.Pack(); err == nil {
			s.codingPack = pack
		}
	}
	return s.codingPack
}

// derivedFrom reports whether b's provenance derives from id.
func derivedFrom(b schema.Block, id string) bool {
	if b.Provenance == nil {
		return false
	}
	for _, d := range b.Provenance.DerivedFrom {
		if d == id {
			return true
		}
	}
	return false
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

// cmdRoute inspects the model routing/tiering policy (AS-042, PRD §7.15) and, with
// arguments, sets a transient per-session override on top of it (AS-110):
//
//	/route                         → render the active policy + recent calls
//	/route <feature> <tier>        → pin a feature to a tier for this session
//	/route <tier> <vendor> <model> → remap a tier's model for a vendor this session
//
// Overrides are copied onto baseRouter (never mutating the shared config policy)
// and reset on /clear and /resume. Inspection stays read-only and model-call-free,
// so the panel opens instantly. When /compact has auto-escalated this session
// (AS-116), the escalations — feature, tiers moved between, and reason — are shown.
func (s *chatSession) cmdRoute(_ context.Context, args []string) (command.Output, error) {
	if len(args) > 0 {
		return s.routeOverride(args)
	}
	summary := cost.Summarize(s.events(), s.pricing)
	turns := summary.Turns
	const maxRecent = 5
	if len(turns) > maxRecent {
		turns = turns[len(turns)-maxRecent:]
	}
	recent := make([]routing.Call, 0, len(turns))
	for _, t := range turns {
		recent = append(recent, routing.Call{Index: t.Index, Model: t.Model})
	}
	// Snapshot the policy under the lock: a concurrent /route override or session
	// swap (/clear, /resume) reassigns s.router, so reading the struct field
	// unguarded is a data race. The maps are copy-on-write, so the snapshot stays
	// valid after the lock is dropped.
	s.mu.Lock()
	router := s.router
	s.mu.Unlock()
	return command.Output{Text: routing.Render(router, recent, routeEscalations(s.events()))}, nil
}

// routeEscalations decodes the session's auto-escalation events into the routing
// render's Escalation view (AS-116), so /route can show that an escalation
// occurred, which tiers it moved between, and the producer's structured reason.
func routeEscalations(events []schema.Block) []routing.Escalation {
	var out []routing.Escalation
	for _, b := range events {
		if e, ok := eventlog.EscalationOf(b); ok {
			out = append(out, routing.Escalation{
				Feature: e.Feature,
				From:    routing.Tier(e.From),
				To:      routing.Tier(e.To),
				Reason:  e.Reason,
			})
		}
	}
	return out
}

// routeOverride applies a transient per-session routing override and renders the
// updated policy (AS-110). Arity disambiguates the two forms: two args are
// `<feature> <tier>` (pin a feature to a tier), three are `<tier> <vendor> <model>`
// (remap a tier's model for a vendor). Each override is layered onto the current
// router via the copy-on-write With* helpers, so the shared config policy is never
// mutated; /clear and /resume reset s.router back to baseRouter.
func (s *chatSession) routeOverride(args []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var note string
	switch len(args) {
	case 2:
		feature := args[0]
		tier, ok := routing.ParseTier(args[1])
		if !ok {
			return command.Output{}, fmt.Errorf("unknown tier %q (cheap|standard|strong)", args[1])
		}
		s.router = s.router.WithFeatureTier(feature, tier)
		note = fmt.Sprintf("Pinned %q to the %s tier for this session.", feature, tier)
	case 3:
		tier, ok := routing.ParseTier(args[0])
		if !ok {
			return command.Output{}, fmt.Errorf("unknown tier %q (cheap|standard|strong)", args[0])
		}
		vendor, model := args[1], args[2]
		if vendor == "" || model == "" {
			return command.Output{}, fmt.Errorf("vendor and model must be non-empty")
		}
		s.router = s.router.WithVendorModel(tier, vendor, model)
		note = fmt.Sprintf("Mapped the %s tier to %s=%s for this session.", tier, vendor, model)
	default:
		return command.Output{}, fmt.Errorf("usage: route <feature> <tier> | route <tier> <vendor> <model>")
	}
	// Read escalations through the Log's own lock (not s.events(), which would
	// re-enter s.mu we already hold here).
	escalations := routeEscalations(s.sess.Log.Events())
	return command.Output{Text: note + " (resets on /clear or /resume)\n\n" + routing.Render(s.router, nil, escalations)}, nil
}

// cmdInsights renders the /insights session retrospective (AS-045, PRD §7.14): a
// dashboard of measured signals (cost, costliest turns, repeated work, oversized
// outputs, error loops, context health) and grounded, applicable suggestions,
// computed from the log alone — no model calls, so the panel opens instantly and
// renders even with no pricing configured. `/insights apply <n>` lands the
// numbered suggestion's memory-file edit through a shown diff. The numbering is
// stable because the analysis is deterministic, so no preview state is staged.
func (s *chatSession) cmdInsights(_ context.Context, args []string) (command.Output, error) {
	if len(args) > 0 {
		if args[0] != "apply" {
			return command.Output{Text: "Usage: /insights [apply <n>]"}, nil
		}
		return s.insightsApply(args[1:])
	}
	s.mu.Lock()
	events := s.sess.Log.Events()
	model := s.model
	table := s.pricing
	s.mu.Unlock()

	return command.Output{Text: insights.Render(insights.Analyze(events, table, model))}, nil
}

// insightsApply lands the memory-file edit of the suggestion at the given 1-based
// index, showing the diff that landed. A suggestion is re-derived from the live
// log (deterministic, so the index is stable), the line is appended to its target
// memory file under the working directory, and a duplicate is reported rather than
// re-appended — the propose-only edit becomes a confirmed write only here, never
// from the sub-agent (D9, C.5).
func (s *chatSession) insightsApply(args []string) (command.Output, error) {
	if len(args) != 1 {
		return command.Output{Text: "Usage: /insights apply <n>"}, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return command.Output{Text: fmt.Sprintf("Not a suggestion number: %q", args[0])}, nil
	}

	s.mu.Lock()
	events := s.sess.Log.Events()
	model := s.model
	table := s.pricing
	wd := s.wd
	s.mu.Unlock()

	rep := insights.Analyze(events, table, model)
	if n > len(rep.Suggestions) {
		return command.Output{Text: fmt.Sprintf("No suggestion #%d (the dashboard lists %d).", n, len(rep.Suggestions))}, nil
	}
	edit := rep.Suggestions[n-1].Edit
	if edit == nil {
		return command.Output{Text: fmt.Sprintf("Suggestion #%d is guidance only — nothing to apply.", n)}, nil
	}

	path := edit.Target
	if !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return command.Output{}, fmt.Errorf("read memory file: %w", err)
	}
	if containsLine(existing, edit.Line) {
		return command.Output{Text: fmt.Sprintf("Already in %s — nothing to apply.", edit.Target)}, nil
	}
	if err := appendMemoryLine(path, existing, edit.Line); err != nil {
		return command.Output{}, fmt.Errorf("write memory file: %w", err)
	}
	return command.Output{Text: fmt.Sprintf("Applied to %s:\n\n  + %s", edit.Target, edit.Line)}, nil
}

// containsLine reports whether content already has line as a whole line (ignoring
// surrounding whitespace), so applying the same suggestion twice is idempotent
// without the false positives a substring match would hit (e.g. "- make test"
// matching an existing "- make test-all").
func containsLine(content []byte, line string) bool {
	want := strings.TrimSpace(line)
	for _, l := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

// appendMemoryLine appends line to the memory file at path, creating it if absent
// and inserting a separating newline only when the existing content does not
// already end with one, so the file stays well-formed Markdown.
func appendMemoryLine(path string, existing []byte, line string) error {
	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(line)
	b.WriteByte('\n')
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// cmdSkills renders the /skills report (AS-050, PRD §7.20): the current session's
// findings plus the cross-session rollup of living-skill signals — the
// rediscovered facts (AS-048) and skill grades (AS-049) the analyzers reported,
// aggregated per project so a fact rediscovered across 3+ sessions is escalated.
// `/skills apply <n>` lands the numbered pending remedy through a shown diff and
// marks the finding resolved. The numbering is stable because the rollup is
// deterministic. With no durable store wired (a face that opted out), it renders
// the session-only view.
func (s *chatSession) cmdSkills(_ context.Context, args []string) (command.Output, error) {
	if len(args) > 0 {
		if args[0] != "apply" {
			return command.Output{Text: "Usage: /skills [apply <n>]"}, nil
		}
		return s.skillsApply(args[1:])
	}
	s.mu.Lock()
	session := s.sess.ID
	store := s.insights
	rollup := s.rollup
	s.mu.Unlock()

	var perSession []subagent.Finding
	if store != nil {
		perSession = store.Findings(session)
	}
	var rep skillrollup.Report
	if rollup != nil {
		rep = rollup.Rollup()
	}
	return command.Output{Text: skillrollup.Render(rep, perSession, session)}, nil
}

// skillsApply lands the pending remedy at the given 1-based index: the rollup is
// recomputed (deterministic, so the index is stable), its proposed line is
// appended to the target file under the working directory, and the finding is
// resolved so it stops pending across sessions. A duplicate line is reported
// rather than re-appended — the propose-only edit becomes a confirmed write only
// here, never from the sub-agent (D9, C.5).
func (s *chatSession) skillsApply(args []string) (command.Output, error) {
	if len(args) != 1 {
		return command.Output{Text: "Usage: /skills apply <n>"}, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return command.Output{Text: fmt.Sprintf("Not a remedy number: %q", args[0])}, nil
	}
	s.mu.Lock()
	rollup := s.rollup
	wd := s.wd
	s.mu.Unlock()
	if rollup == nil {
		return command.Output{Text: "No cross-session findings store — nothing to apply."}, nil
	}

	rep := rollup.Rollup()
	if n > len(rep.Pending) {
		return command.Output{Text: fmt.Sprintf("No remedy #%d (the report lists %d).", n, len(rep.Pending))}, nil
	}
	p := rep.Pending[n-1]
	target := p.Target
	if target == "" {
		return command.Output{Text: fmt.Sprintf("Remedy #%d names no target file — nothing to apply.", n)}, nil
	}
	path, ok := resolveApplyTarget(wd, target)
	if !ok {
		return command.Output{Text: fmt.Sprintf("Refusing to apply: target %q escapes the working directory.", target)}, nil
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return command.Output{}, fmt.Errorf("read skill/memory file: %w", err)
	}
	if containsLine(existing, p.Diff) {
		if err := rollup.Resolve(p.Kind, p.Summary); err != nil {
			return command.Output{}, fmt.Errorf("resolve finding: %w", err)
		}
		return command.Output{Text: fmt.Sprintf("Already in %s — marked resolved.", target)}, nil
	}
	if err := appendMemoryLine(path, existing, p.Diff); err != nil {
		return command.Output{}, fmt.Errorf("write skill/memory file: %w", err)
	}
	if err := rollup.Resolve(p.Kind, p.Summary); err != nil {
		return command.Output{}, fmt.Errorf("resolve finding: %w", err)
	}
	return command.Output{Text: fmt.Sprintf("Applied to %s and marked resolved:\n\n  %s", target, p.Diff)}, nil
}

// resolveApplyTarget resolves a remedy's target file to an absolute path and
// reports whether it is safe to write. A relative target is joined under the
// working directory and rejected if it escapes it (a `../` traversal in a
// tampered findings log must not write outside the project) — defense in depth,
// since targets come from our own analyzers. An absolute target is accepted as
// cleaned: a remedy may legitimately point at a user/project skill's SKILL.md,
// which lives outside the working directory (the skill dirs).
func resolveApplyTarget(wd, target string) (string, bool) {
	if filepath.IsAbs(target) {
		return filepath.Clean(target), true
	}
	path := filepath.Join(wd, target)
	rel, err := filepath.Rel(wd, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}

// cmdClean is the manual context editor (AS-028 /clean, PRD §7.12): the user
// selects live segments by their /context handle, sees a preview of exactly what
// leaves the window and the tokens/$ reclaimed, then confirms. Removal is an
// appended exclusion event — history is never mutated (D3) — and /clean --undo
// restores the most recent removal exactly.
//
//   - /clean <handle>…  preview the removal (mutates nothing) and stage it
//   - /clean "<topic>"  preview removing segments a topic query matches (AS-029)
//   - /clean --apply     confirm the staged preview, appending the exclusion
//   - /clean --undo      restore the most recent removal
//   - /clean --cancel    discard the staged preview
func (s *chatSession) cmdClean(ctx context.Context, args []string) (command.Output, error) {
	switch f := command.FlagsFrom(ctx); {
	case f.Bool("apply"):
		return s.cleanApply()
	case f.Bool("undo"):
		return s.cleanUndo()
	case f.Bool("cancel"):
		return s.cleanCancel()
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
//
// args are tried first as block handles (the AS-028 path). When none resolve,
// they are taken as a natural-language topic query and matched with the AS-029
// engine — so `/clean "the bug we fixed"` selects the related segments while an
// exact handle stays exact. Either way nothing auto-removes: the staged preview
// awaits /clean --apply (AC: preview before apply, nothing lost).
func (s *chatSession) cleanPreview(handles []string) (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proj := projection.Project(s.sess.Log.Events(), projection.Options{TargetModel: s.model})
	plan := clean.Preview(proj, s.pricing, s.model, time.Now(), handles)
	if plan.Empty() {
		if q := strings.TrimSpace(strings.Join(handles, " ")); q != "" {
			plan = clean.PreviewMatch(proj, s.pricing, s.model, time.Now(), q)
		}
	}
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

// cmdInit implements /init (AS-039): bootstrap the project for Agent Smith.
// Bare /init scans the repo and stages a preview of the files it would write —
// an AGENT.md memory file (amending an existing one rather than clobbering it)
// and the .agent-smith/ scaffold — without touching the filesystem. The writes
// happen only on /init --apply; /init --cancel discards the staged plan. A
// re-run on an initialized project proposes only the missing pieces.
func (s *chatSession) cmdInit(ctx context.Context, _ []string) (command.Output, error) {
	switch f := command.FlagsFrom(ctx); {
	case f.Bool("apply"):
		return s.initApply()
	case f.Bool("cancel"):
		return s.initCancel()
	}
	return s.initPreview()
}

// initPreview scans the working directory and stages the resulting plan for
// confirmation. Nothing is written; the plan is keyed to the active session so a
// /clear or /resume before --apply invalidates it.
func (s *chatSession) initPreview() (command.Output, error) {
	// Scan the filesystem outside the lock — s.wd is immutable — so a slow disk
	// scan never blocks the status-line Meter/Meta refresh that also takes s.mu.
	plan, err := initscaffold.Scan(s.wd)
	if err != nil {
		return command.Output{}, fmt.Errorf("scan project: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.Empty() {
		s.pendingInit, s.pendingInitFor = nil, nil
		return command.Output{Text: plan.Render()}, nil
	}
	s.pendingInit, s.pendingInitFor = &plan, s.sess
	return command.Output{Text: plan.Render()}, nil
}

// initApply writes the staged scaffold to disk. The plan is discarded once
// applied, so a second --apply is a no-op rather than a double write. When no
// valid plan is staged for this session — e.g. a scripted `smith init --apply`
// in a fresh process, or after a /clear invalidated the staged one — it re-scans
// and applies, which is safe because the scan is deterministic and never clobbers.
func (s *chatSession) initApply() (command.Output, error) {
	// Take the staged plan under the lock, then release it: the scan and the
	// file writes below are blocking disk I/O and must not hold s.mu, or they
	// would stall the status-line Meter/Meta refresh that shares it.
	s.mu.Lock()
	var plan initscaffold.Plan
	hasPending := s.pendingInit != nil && s.pendingInitFor == s.sess
	if hasPending {
		plan = *s.pendingInit
	}
	s.mu.Unlock()

	if !hasPending {
		fresh, err := initscaffold.Scan(s.wd)
		if err != nil {
			return command.Output{}, fmt.Errorf("scan project: %w", err)
		}
		plan = fresh
	}
	if plan.Empty() {
		s.clearPendingInit()
		return command.Output{Text: plan.Render()}, nil
	}
	if err := plan.Apply(); err != nil {
		return command.Output{}, fmt.Errorf("write scaffold: %w", err)
	}
	s.clearPendingInit()
	var b strings.Builder
	b.WriteString("Wrote:\n")
	for _, c := range plan.Changes {
		b.WriteString("  · " + c.Rel + "\n")
	}
	b.WriteString("The memory file is picked up automatically next session (or after /clear).")
	return command.Output{Text: b.String()}, nil
}

// clearPendingInit drops any staged scaffold under the lock, but only when it
// still belongs to the active session — a /clear or /resume during the unlocked
// Apply may have staged a new one we must not clobber.
func (s *chatSession) clearPendingInit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingInitFor == s.sess {
		s.pendingInit, s.pendingInitFor = nil, nil
	}
}

// initCancel discards a staged scaffold without touching the filesystem.
func (s *chatSession) initCancel() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingInit == nil {
		return command.Output{Text: "Nothing staged to cancel."}, nil
	}
	s.pendingInit, s.pendingInitFor = nil, nil
	return command.Output{Text: "Discarded the staged /init scaffold. Nothing changed."}, nil
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

// cmdTidy is the context reorganizer (AS-043 /tidy, PRD §7.13): the V1 mechanical
// half dedupes identical file reads — keeping the latest read of each path and
// dropping the earlier ones — without lossy summarization. The reclaim is an
// appended exclusion event, so history is never mutated (D3); the preview is a
// fidelity diff and /tidy --undo restores the most recent dedup exactly.
//
//   - /tidy           preview the dedup (mutates nothing) and stage it
//   - /tidy --apply   confirm the staged preview, appending the exclusion
//   - /tidy --undo    restore the most recent dedup
//   - /tidy --cancel  discard the staged preview
func (s *chatSession) cmdTidy(ctx context.Context, _ []string) (command.Output, error) {
	switch f := command.FlagsFrom(ctx); {
	case f.Bool("apply"):
		return s.tidyApply()
	case f.Bool("undo"):
		return s.tidyUndo()
	case f.Bool("cancel"):
		return s.tidyCancel()
	}
	return s.tidyPreview()
}

// tidyPreview stages a dedup: it projects the live window, builds the plan, and
// stores it pending confirmation. Nothing is appended to the log.
func (s *chatSession) tidyPreview() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proj := projection.Project(s.sess.Log.Events(), projection.Options{TargetModel: s.model})
	plan := tidy.Preview(proj, s.pricing, s.model, time.Now())
	if plan.Empty() {
		s.pendingTidy, s.pendingTidyFor = nil, nil
		return command.Output{Text: tidy.RenderPreview(plan)}, nil
	}
	s.pendingTidy, s.pendingTidyFor = &plan, s.sess
	return command.Output{Text: tidy.RenderPreview(plan)}, nil
}

// tidyApply confirms the staged preview, appending the exclusion event that
// drops the older reads from the projection. The plan is discarded once applied.
func (s *chatSession) tidyApply() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingTidy == nil {
		return command.Output{Text: "Nothing staged. Run /tidy to preview a dedup first."}, nil
	}
	if s.pendingTidyFor != s.sess {
		s.pendingTidy, s.pendingTidyFor = nil, nil
		return command.Output{Text: "The staged preview was for a different session and is no longer valid. Run /tidy again."}, nil
	}
	plan := *s.pendingTidy
	event, ok := tidy.Apply(plan)
	if !ok {
		s.pendingTidy, s.pendingTidyFor = nil, nil
		return command.Output{Text: "Nothing to apply."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record exclusion: %w", err)
	}
	s.pendingTidy, s.pendingTidyFor = nil, nil
	return command.Output{Text: fmt.Sprintf("Deduped %s, reclaiming %d tokens. Restore with /tidy --undo.",
		pluralReads(plan.DroppedCount()), plan.Tokens)}, nil
}

// tidyUndo restores the most recent /tidy dedup by appending a counter-exclusion.
// The log is never rewritten, so the restoration is exact.
func (s *chatSession) tidyUndo() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, removed, ok := tidy.Undo(s.sess.Log.Events())
	if !ok {
		return command.Output{Text: "No /tidy dedup to undo in this session."}, nil
	}
	if _, err := s.sess.Log.Append(event); err != nil {
		return command.Output{}, fmt.Errorf("record undo: %w", err)
	}
	return command.Output{Text: fmt.Sprintf("Restored %s to the window.", pluralReads(removed))}, nil
}

// tidyCancel discards a staged preview without touching the log.
func (s *chatSession) tidyCancel() (command.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingTidy == nil {
		return command.Output{Text: "Nothing staged to cancel."}, nil
	}
	s.pendingTidy, s.pendingTidyFor = nil, nil
	return command.Output{Text: "Discarded the staged /tidy preview. Nothing changed."}, nil
}

// pluralReads labels a file-read count for the tidy confirm/undo lines.
func pluralReads(n int) string {
	if n == 1 {
		return "1 read"
	}
	return strconv.Itoa(n) + " reads"
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
  /clean "<topic>"   preview removing segments matching a topic, e.g.
                     /clean "the bug we fixed" — matches are explained, nothing auto-removes
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
func (s *chatSession) cmdRewind(ctx context.Context, args []string) (command.Output, error) {
	switch f := command.FlagsFrom(ctx); {
	case f.Bool("apply"):
		return s.rewindApply()
	case f.Bool("undo"):
		return s.rewindUndo()
	case f.Bool("cancel"):
		return s.rewindCancel()
	case f.Set("mark"):
		// The label travels with the flag through the shared string path; an empty
		// one is "mark requested" and rewindMark explains the label is required.
		return s.rewindMark(f.String("mark"))
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
func (s *chatSession) cmdCompact(ctx context.Context, _ []string) (command.Output, error) {
	switch f := command.FlagsFrom(ctx); {
	case f.Bool("apply"):
		return s.compactApply(ctx)
	case f.Bool("undo"):
		return s.compactUndo()
	case f.Bool("cancel"):
		return s.compactCancel()
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
	vendor := s.provName
	fallback := s.model
	router := s.router
	baseTier := router.FeatureTier("compact", routing.Cheap)
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

	// /compact is a tier-declared, model-using task (AS-042), so route its attempt
	// through the tier policy and auto-escalate once when the summarizer comes back
	// empty — a structured low-confidence result — to the next stronger tier
	// (AS-110 primitive, AS-116 first producer). This is explicit and feature-owned
	// (a user-invoked /compact), never an invisible retry for a normal chat turn.
	// Each attempt records its own usage event, so /cost attributes the retry's
	// extra spend to the escalated turn, and the escalation is logged so /route can
	// show it with the producer's structured reason (§9: grounded, never invented).
	var summary string
	var summarizeErr error
	attempt := func(tier routing.Tier) routing.Attempt {
		model := router.Resolve(tier, vendor, fallback)
		sum, tokens, stopReason, aerr := summarize(ctx, prov, model, plan.Sources)
		if aerr != nil {
			// A transport/provider error is not a low-confidence result: don't
			// escalate it. Report OK so Escalate stops, then the check below aborts.
			summarizeErr = aerr
			return routing.Attempt{OK: true}
		}
		if tokens != nil {
			if _, err := log.Append(eventlog.NewUsage(compact.Producer, vendor, model, stopReason, tokens, nil)); err != nil {
				summarizeErr = err
				return routing.Attempt{OK: true}
			}
		}
		if strings.TrimSpace(sum) == "" {
			return routing.Attempt{OK: false, Reason: "the summarizer returned an empty summary"}
		}
		summary = sum
		return routing.Attempt{OK: true}
	}
	res, esc := routing.Escalate("compact", baseTier, attempt)
	if summarizeErr != nil {
		return command.Output{}, fmt.Errorf("summarize for /compact: %w", summarizeErr)
	}
	if esc != nil {
		if _, err := log.Append(eventlog.NewEscalation(compact.Producer, eventlog.Escalation{
			Feature: esc.Feature, From: string(esc.From), To: string(esc.To), Reason: esc.Reason,
		})); err != nil {
			return command.Output{}, fmt.Errorf("record escalation: %w", err)
		}
	}
	block, ok := compact.Build(plan, summary)
	if !res.OK || !ok {
		s.clearPendingCompact()
		return command.Output{Text: "The summarizer returned nothing; the conversation was left unchanged."}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// The lock was dropped for the (slow) summarization, so a concurrent /clear or
	// /resume may have swapped s.sess. The block carries the old session's source
	// IDs, so appending it to a different session would corrupt that log; abandon
	// the compaction instead, mirroring the staged-preview "session changed" guard.
	if s.sess.ID != sessID {
		s.pendingCompact, s.pendingCompactFor = nil, nil
		return command.Output{Text: "The session changed during compaction; the compaction was abandoned. Run /compact again."}, nil
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

// cheapModel resolves the model to summarize with, keeping the active vendor (so
// its provider is already configured) and routing through the tier policy
// (AS-042): /compact defaults to the cheap tier (AS-038 AC4), which config can
// remap. An unmapped vendor/tier falls back to the active model rather than
// guessing an id the provider would reject — the default policy reproduces the
// previous per-vendor cheap families exactly.
func (s *chatSession) cheapModel() (vendor, model string) {
	tier := s.router.FeatureTier("compact", routing.Cheap)
	return s.provName, s.router.Resolve(tier, s.provName, s.model)
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
	// Drop any per-session /route overrides: they are transient and must not leak
	// into the fresh session (AS-110 AC). The durable config policy is unchanged.
	s.router = s.baseRouter
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
	// Drop per-session /route overrides on a session swap (AS-110 AC): overrides
	// belong to the session that set them, not the one being resumed.
	s.router = s.baseRouter
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
