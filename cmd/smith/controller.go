package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/clean"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/goal"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/permission"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/session"
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
func newChatSession(store *session.Store, tools *tool.Registry, pricing *cost.Table, providers map[string]provider.Provider, sess *session.Session, provName, model, wd string) *chatSession {
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
	}
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
	return nil
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
	rt := tool.NewRuntime(s.tools, sess.Log, rtOpts...)
	return loop.New(prov, sess.Log, rt, s.tools, model, loop.WithObserver(s.observer))
}

// Run drives one user turn against the current engine (tui.Runner). It reads the
// engine under the lock and releases it before the turn so a long turn does not
// serialize the status-line meter or a concurrent command dispatch.
func (s *chatSession) Run(ctx context.Context, userText string) (loop.Result, error) {
	s.mu.Lock()
	eng := s.engine
	s.mu.Unlock()
	return eng.Run(ctx, userText)
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
	s.meterCache = tui.Meter{
		Tokens:    used,
		Window:    window,
		CostUSD:   summary.TotalUSD,
		CostKnown: summary.AllPriced,
		Currency:  cost.Symbol(summary.Currency),
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
