package main

import (
	"context"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/goal"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/mode"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/openai"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// newTestController builds a chatSession over a temp-rooted store with the real
// (network-free, since no turn is driven) Anthropic and OpenAI providers, so the
// AS-023 parity commands can be exercised without a TUI or live API.
func newTestController(t *testing.T) *chatSession {
	t.Helper()
	store, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	sess, err := store.Create("")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	providers := map[string]provider.Provider{
		"anthropic": anthropic.New("k"),
		"openai":    openai.New("k"),
	}
	ctl := newChatSession(store, tool.NewRegistry(), cost.Embedded(), providers, sess, "anthropic", "claude-opus-4-8", t.TempDir(), nil, nil)
	if err := ctl.start(func(loop.UIEvent) {}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return ctl
}

// appendUserText writes a user text block to the controller's current session
// log, standing in for a turn so the projection has content to preserve.
func appendUserText(t *testing.T, ctl *chatSession, text string) {
	t.Helper()
	_, err := ctl.sess.Log.Append(schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindText,
		Role: schema.RoleUser,
		Text: &schema.TextBody{Text: text, Subtype: schema.TextSubtypeNormal},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
}

// appendUserTextID writes a user text block with a chosen ID so a test can name
// the /clean handle to select.
func appendUserTextID(t *testing.T, ctl *chatSession, id, text string) {
	t.Helper()
	_, err := ctl.sess.Log.Append(schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: schema.RoleUser,
		Text: &schema.TextBody{Text: text, Subtype: schema.TextSubtypeNormal},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
}

func liveContains(t *testing.T, ctl *chatSession, id string) bool {
	t.Helper()
	for _, b := range projection.Project(ctl.sess.Log.Events(), projection.Options{}).Live() {
		if b.ID == id {
			return true
		}
	}
	return false
}

// TestCleanSelectorBuildsAndApplies covers AS-068: the no-arg /clean exposes an
// interactive selector whose live segments are selectable, whose Apply commits
// the selection as one exclusion through the same engine, and whose Restore
// re-includes a single excluded block from the archive.
func TestCleanSelectorBuildsAndApplies(t *testing.T) {
	ctl := newTestController(t)
	appendUserTextID(t, ctl, "blk_keepme00", "keep this one")
	appendUserTextID(t, ctl, "blk_dropme00", strings.Repeat("drop this content ", 20))

	out, err := runClean(t, ctl)
	if err != nil {
		t.Fatalf("/clean (no args): %v", err)
	}
	if out.Selector == nil {
		t.Fatal("no-arg /clean did not offer an interactive selector")
	}
	// The live segments are selectable items (no archive yet).
	var dropValue string
	for _, it := range out.Selector.Items {
		if it.Value == "blk_dropme00" {
			dropValue = it.Value
		}
	}
	if dropValue == "" {
		t.Fatalf("selector items missing blk_dropme00: %+v", out.Selector.Items)
	}

	// Preview is live and mutates nothing.
	before := ctl.sess.Log.Len()
	prev := out.Selector.Preview([]string{dropValue})
	if !strings.Contains(prev.Summary, "tok reclaimed") {
		t.Fatalf("preview summary = %q, want a reclaim figure", prev.Summary)
	}
	if ctl.sess.Log.Len() != before {
		t.Fatal("Preview mutated the log")
	}

	// Apply removes the block via one appended exclusion.
	msg := out.Selector.Apply([]string{dropValue})
	if !strings.Contains(msg, "Removed") {
		t.Fatalf("apply result = %q", msg)
	}
	if liveContains(t, ctl, "blk_dropme00") {
		t.Fatal("blk_dropme00 still live after selector apply")
	}
	if ctl.sess.Log.Len() != before+1 {
		t.Fatalf("apply appended %d events, want 1", ctl.sess.Log.Len()-before)
	}

	// The excluded block now appears in a fresh selector's archive and restores.
	out2, err := runClean(t, ctl)
	if err != nil {
		t.Fatalf("/clean (no args) after apply: %v", err)
	}
	var archived bool
	for _, it := range out2.Selector.Archive {
		if it.Value == "blk_dropme00" {
			archived = true
		}
	}
	if !archived {
		t.Fatalf("excluded block missing from archive: %+v", out2.Selector.Archive)
	}
	if rmsg := out2.Selector.Restore("blk_dropme00"); !strings.Contains(rmsg, "Restored") {
		t.Fatalf("restore result = %q", rmsg)
	}
	if !liveContains(t, ctl, "blk_dropme00") {
		t.Fatal("blk_dropme00 not restored to the window")
	}
}

// runClean dispatches /clean exactly as a face does (AS-104).
func runClean(t *testing.T, ctl *chatSession, args ...string) (command.Output, error) {
	t.Helper()
	return runChatCommand(t, ctl, "clean", args...)
}

// runChatCommand dispatches a registered chat command exactly as a face does
// (AS-104/AS-105): it looks up the command and parses the args through its
// declared flags before running the handler, so a test exercises the real
// flag-parsing path (--apply/--undo/--cancel, --mark "<label>") instead of
// reaching past it into the handler.
func runChatCommand(t *testing.T, ctl *chatSession, name string, args ...string) (command.Output, error) {
	t.Helper()
	c, ok := chatCommands(ctl).Lookup(name)
	if !ok {
		t.Fatalf("%s not registered", name)
	}
	ctx, rest, err := c.ParseFlags(context.TODO(), args)
	if err != nil {
		return command.Output{}, err
	}
	return c.Run(ctx, rest)
}

// TestCleanPreviewApplyUndo covers the /clean wiring (AS-028): a preview stages
// the removal without touching the log, --apply drops the block from the window
// via an appended exclusion, and --undo restores it exactly.
func TestCleanPreviewApplyUndo(t *testing.T) {
	ctl := newTestController(t)
	appendUserTextID(t, ctl, "blk_keepme00", "keep this one")
	appendUserTextID(t, ctl, "blk_dropme00", strings.Repeat("drop this content ", 20))

	before := ctl.sess.Log.Len()
	out, err := runClean(t, ctl, "blk_dropme00")
	if err != nil {
		t.Fatalf("/clean preview: %v", err)
	}
	if !strings.Contains(out.Text, "Preview") || !strings.Contains(out.Text, "reclaim") {
		t.Errorf("preview missing its summary:\n%s", out.Text)
	}
	if ctl.sess.Log.Len() != before {
		t.Fatal("/clean preview must not append to the log")
	}
	if ctl.pendingClean == nil {
		t.Fatal("/clean preview did not stage a pending plan")
	}

	if _, err := runClean(t, ctl, "--apply"); err != nil {
		t.Fatalf("/clean --apply: %v", err)
	}
	if liveContains(t, ctl, "blk_dropme00") {
		t.Error("block still live after /clean --apply")
	}
	if !liveContains(t, ctl, "blk_keepme00") {
		t.Error("unrelated block dropped by /clean --apply")
	}
	if ctl.pendingClean != nil {
		t.Error("pending plan not cleared after apply")
	}

	if _, err := runClean(t, ctl, "--undo"); err != nil {
		t.Fatalf("/clean --undo: %v", err)
	}
	if !liveContains(t, ctl, "blk_dropme00") {
		t.Error("block not restored after /clean --undo")
	}
}

// TestCleanCancelAndInvalidation covers the staging guards: --cancel discards a
// preview, and a session swap (/clear) invalidates a staged plan so --apply can
// never remove blocks from the wrong log.
func TestCleanCancelAndInvalidation(t *testing.T) {
	ctl := newTestController(t)
	appendUserTextID(t, ctl, "blk_target00", strings.Repeat("content ", 20))

	if _, err := runClean(t, ctl, "blk_target00"); err != nil {
		t.Fatalf("/clean preview: %v", err)
	}
	if _, err := runClean(t, ctl, "--cancel"); err != nil {
		t.Fatalf("/clean --cancel: %v", err)
	}
	if ctl.pendingClean != nil {
		t.Error("--cancel did not clear the pending plan")
	}

	// Stage again, then swap sessions; --apply must refuse the stale plan.
	if _, err := runClean(t, ctl, "blk_target00"); err != nil {
		t.Fatalf("/clean preview: %v", err)
	}
	if _, err := ctl.cmdClear(context.TODO(), nil); err != nil {
		t.Fatalf("/clear: %v", err)
	}
	out, err := runClean(t, ctl, "--apply")
	if err != nil {
		t.Fatalf("/clean --apply after /clear: %v", err)
	}
	if !strings.Contains(out.Text, "no longer valid") {
		t.Errorf("stale apply should be refused, got:\n%s", out.Text)
	}
	if ctl.sess.Log.Len() != 0 {
		t.Error("stale --apply must not append to the fresh session log")
	}
}

// TestContextComposition covers the /context wiring (AS-026): the command runs
// over the live session log with no model call and renders the composition panel
// reflecting what was appended.
func TestContextComposition(t *testing.T) {
	ctl := newTestController(t)
	appendUserText(t, ctl, "what is filling my window")

	out, err := ctl.cmdContext(context.TODO(), nil)
	if err != nil {
		t.Fatalf("/context: %v", err)
	}
	for _, want := range []string{"Context composition", "Top consumers", "user"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("/context output missing %q:\n%s", want, out.Text)
		}
	}

	// An unknown sort argument falls back to the default rather than erroring.
	if _, err := ctl.cmdContext(context.TODO(), []string{"bogus"}); err != nil {
		t.Fatalf("/context bogus arg: %v", err)
	}
}

// TestClearStartsFreshAndKeepsOld covers AC1: /clear starts a clean context and
// the previous session remains discoverable via /resume.
func TestClearStartsFreshAndKeepsOld(t *testing.T) {
	ctl := newTestController(t)
	oldID := ctl.sess.ID
	appendUserText(t, ctl, "hello from the first session")

	out, err := ctl.cmdClear(context.TODO(), nil)
	if err != nil {
		t.Fatalf("/clear: %v", err)
	}
	if !out.ResetView {
		t.Error("/clear should reset the view for a clean context")
	}
	if ctl.sess.ID == oldID {
		t.Fatal("/clear did not start a new session")
	}
	if got := ctl.sess.Log.Len(); got != 0 {
		t.Errorf("new session log length = %d, want 0 (clean context)", got)
	}

	// The previous session must still be listed (and the new one too).
	summaries, err := ctl.store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !containsSession(summaries, oldID) {
		t.Errorf("previous session %s not found in /resume listing", oldID)
	}
}

// TestModelSwitchRecordsEvent covers AC2: /model switches provider/model and the
// switch is recorded on the event log.
func TestModelSwitchRecordsEvent(t *testing.T) {
	ctl := newTestController(t)

	out, err := ctl.cmdModel(context.TODO(), []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("/model: %v", err)
	}
	if ctl.provName != "openai" || ctl.model != "gpt-4o" {
		t.Errorf("after switch: provider=%q model=%q, want openai/gpt-4o", ctl.provName, ctl.model)
	}
	if out.Text == "" {
		t.Error("/model switch produced no confirmation text")
	}

	events := ctl.sess.Log.Events()
	var switches int
	for _, b := range events {
		if b.Kind == eventlog.KindModelSwitch {
			switches++
			if b.Provider == nil || b.Provider.Model != "gpt-4o" || b.Provider.Vendor != "openai" {
				t.Errorf("model-switch event records %+v, want openai/gpt-4o", b.Provider)
			}
		}
	}
	if switches != 1 {
		t.Errorf("model-switch events = %d, want 1", switches)
	}

	// An unknown model is rejected, not silently applied.
	if _, err := ctl.cmdModel(context.TODO(), []string{"not-a-real-model"}); err == nil {
		t.Error("/model accepted an unknown model")
	}
}

// TestModelSwitchStaysOutOfProjection guards that the recorded switch is a
// control event the projection never renders into model-facing context.
func TestModelSwitchStaysOutOfProjection(t *testing.T) {
	ctl := newTestController(t)
	appendUserText(t, ctl, "first")
	if _, err := ctl.cmdModel(context.TODO(), []string{"gpt-4o"}); err != nil {
		t.Fatalf("/model: %v", err)
	}
	proj := projection.Project(ctl.sess.Log.Events(), projection.Options{})
	for _, b := range proj.Live() {
		if b.Kind == eventlog.KindModelSwitch {
			t.Fatal("model-switch event leaked into the live projection")
		}
	}
}

// TestCrossProviderResumeKeepsProjection covers AC3 and AC4: a session that
// started on Anthropic, switched to OpenAI, and accrued mixed-vendor blocks
// resumes without corruption and projects identically to its last live state.
func TestCrossProviderResumeKeepsProjection(t *testing.T) {
	ctl := newTestController(t)
	targetID := ctl.sess.ID

	// A turn on Anthropic, a switch to OpenAI, then a turn on OpenAI.
	appendUserText(t, ctl, "anthropic turn")
	if _, err := ctl.cmdModel(context.TODO(), []string{"gpt-4o"}); err != nil {
		t.Fatalf("/model: %v", err)
	}
	appendUserText(t, ctl, "openai turn")
	wantLive := projection.Project(ctl.sess.Log.Events(), projection.Options{}).Live()

	// Move away with /clear, then resume the original by ID.
	if _, err := ctl.cmdClear(context.TODO(), nil); err != nil {
		t.Fatalf("/clear: %v", err)
	}
	out, err := ctl.cmdResume(context.TODO(), []string{targetID})
	if err != nil {
		t.Fatalf("/resume: %v", err)
	}
	if !out.ResetView {
		t.Error("/resume should reset the view to the restored session")
	}
	if ctl.sess.ID != targetID {
		t.Fatalf("resumed %s, want %s", ctl.sess.ID, targetID)
	}
	// The model is restored to the one the session last used (OpenAI's gpt-4o).
	if ctl.provName != "openai" || ctl.model != "gpt-4o" {
		t.Errorf("resumed on %s/%s, want openai/gpt-4o", ctl.provName, ctl.model)
	}

	gotLive := projection.Project(ctl.sess.Log.Events(), projection.Options{}).Live()
	if !blocksEqual(gotLive, wantLive) {
		t.Errorf("resumed projection differs from last live state:\n got %d blocks\nwant %d blocks", len(gotLive), len(wantLive))
	}
}

// TestResumeListingAndUnknownID checks the listing shows sessions and a bad ID
// errors rather than swapping to an invalid session.
func TestResumeListingAndUnknownID(t *testing.T) {
	ctl := newTestController(t)
	out, err := ctl.cmdResume(context.TODO(), nil)
	if err != nil {
		t.Fatalf("/resume listing: %v", err)
	}
	if out.Text == "" {
		t.Error("/resume listing was empty")
	}
	if _, err := ctl.cmdResume(context.TODO(), []string{"sess_does_not_exist"}); err == nil {
		t.Error("/resume accepted an unknown session id")
	}
}

// TestResumeOffersPicker covers AC1's data path: the no-arg /resume returns an
// interactive Picker alongside the scriptable text listing, with one item per
// session whose Value is the full session ID to re-dispatch (the TUI turns the
// list into a keyboard-navigable picker; the headless face renders the text).
func TestResumeOffersPicker(t *testing.T) {
	ctl := newTestController(t)
	first := ctl.sess.ID
	appendUserText(t, ctl, "first session work")
	if _, err := ctl.cmdClear(context.TODO(), nil); err != nil {
		t.Fatalf("/clear: %v", err)
	}
	second := ctl.sess.ID

	out := ctl.resumeList()
	if out.Picker == nil {
		t.Fatal("/resume offered no picker")
	}
	if len(out.Picker.Items) != 2 {
		t.Fatalf("picker has %d items, want 2 sessions", len(out.Picker.Items))
	}
	got := map[string]bool{}
	for _, it := range out.Picker.Items {
		if it.Value == "" {
			t.Error("picker item has empty Value; nothing to re-dispatch /resume with")
		}
		got[it.Value] = true
	}
	if !got[first] || !got[second] {
		t.Errorf("picker missing a session: items=%v, want %s and %s", got, first, second)
	}
	if out.Text == "" {
		t.Error("/resume dropped the scriptable text listing")
	}
}

// TestRehydrateMatchesLiveProjection covers the rehydration data source (AC2/AC4):
// the blocks the face replays are exactly the active session's live projection,
// so a resumed transcript matches its last live state and a fresh session
// rehydrates to nothing.
func TestRehydrateMatchesLiveProjection(t *testing.T) {
	ctl := newTestController(t)
	appendUserText(t, ctl, "a turn worth replaying")
	want := projection.Project(ctl.sess.Log.Events(), projection.Options{TargetModel: ctl.model}).Live()
	got := ctl.rehydrate()
	if !blocksEqual(got, want) {
		t.Errorf("rehydrate returned %d blocks, want the %d live blocks", len(got), len(want))
	}

	if _, err := ctl.cmdClear(context.TODO(), nil); err != nil {
		t.Fatalf("/clear: %v", err)
	}
	if fresh := ctl.rehydrate(); len(fresh) != 0 {
		t.Errorf("a fresh session rehydrated %d blocks, want none", len(fresh))
	}
}

// TestModelRejectsWildcardPattern guards that a pricing-table family pattern
// can't become the active model (it would fail on the next turn).
func TestModelRejectsWildcardPattern(t *testing.T) {
	ctl := newTestController(t)
	if _, err := ctl.cmdModel(context.TODO(), []string{"gpt-4o*"}); err == nil {
		t.Error("/model accepted a family pattern as a concrete model")
	}
	if ctl.model != "claude-opus-4-8" {
		t.Errorf("model changed to %q after a rejected pattern", ctl.model)
	}
}

// TestResumeListingShowsCost guards that the /resume listing carries a cost
// signal (the ticket specifies title, age, cost, size).
func TestResumeListingShowsCost(t *testing.T) {
	ctl := newTestController(t)
	out, err := ctl.cmdResume(context.TODO(), nil)
	if err != nil {
		t.Fatalf("/resume listing: %v", err)
	}
	if !strings.Contains(out.Text, "$") {
		t.Errorf("/resume listing missing a cost signal:\n%s", out.Text)
	}
}

func containsSession(summaries []session.Summary, id string) bool {
	for _, s := range summaries {
		if s.ID == id {
			return true
		}
	}
	return false
}

func blocksEqual(a, b []schema.Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Kind != b[i].Kind || a[i].Seq != b[i].Seq {
			return false
		}
	}
	return true
}

// TestCmdBudget exercises the /budget command (AS-041): show when unset, set a
// ceiling, reflect it on the log, and clear it with "off".
// TestCmdFeatureModePhase exercises the Coding Mode shell (AS-072) end to end
// through its commands: /feature sets the goal and enters at think, /phase moves
// soft-ly through the phase list, and /mode off exits while phase history stays
// on the log — all derived from events, never stored side-state (D3).
func TestCmdFeatureModePhase(t *testing.T) {
	ctl := newTestController(t)
	ctx := context.Background()

	// No mode yet.
	out, err := ctl.cmdMode(ctx, nil)
	if err != nil {
		t.Fatalf("cmdMode show: %v", err)
	}
	if !strings.Contains(out.Text, "No coding mode active") {
		t.Errorf("unset /mode = %q, want a 'No coding mode active' notice", out.Text)
	}

	// /feature sets the goal and enters at the first phase.
	if _, err := ctl.cmdFeature(ctx, []string{"add", "OAuth", "login"}); err != nil {
		t.Fatalf("cmdFeature: %v", err)
	}
	events := ctl.sess.Log.Events()
	if g, ok := goal.Current(events); !ok || g.Objective != "add OAuth login" {
		t.Errorf("goal after /feature = (%+v, %v), want 'add OAuth login'", g, ok)
	}
	cur, ok := mode.Current(events)
	if !ok || cur.Mode != mode.Coding || cur.Phase != "think" {
		t.Fatalf("mode after /feature = (%+v, %v), want coding/think", cur, ok)
	}

	// A second /feature does not silently replace; it asks the user to exit.
	out, err = ctl.cmdFeature(ctx, []string{"something", "else"})
	if err != nil {
		t.Fatalf("second cmdFeature: %v", err)
	}
	if !strings.Contains(out.Text, "Already in") {
		t.Errorf("second /feature = %q, want an 'Already in' notice", out.Text)
	}

	// /phase next advances; /phase <name> jumps; nothing gates.
	if _, err := ctl.cmdPhase(ctx, []string{"next"}); err != nil {
		t.Fatalf("cmdPhase next: %v", err)
	}
	if cur, _ := mode.Current(ctl.sess.Log.Events()); cur.Phase != "analyse" {
		t.Errorf("phase after next = %q, want analyse", cur.Phase)
	}
	if _, err := ctl.cmdPhase(ctx, []string{"verify"}); err != nil {
		t.Fatalf("cmdPhase verify: %v", err)
	}
	if cur, _ := mode.Current(ctl.sess.Log.Events()); cur.Phase != "verify" {
		t.Errorf("phase after jump = %q, want verify", cur.Phase)
	}

	// An unknown phase is rejected (a typo never creates a junk phase).
	if _, err := ctl.cmdPhase(ctx, []string{"ship"}); err == nil {
		t.Error("cmdPhase accepted an unknown phase")
	}

	// /mode off exits; phase history survives.
	if _, err := ctl.cmdMode(ctx, []string{"off"}); err != nil {
		t.Fatalf("cmdMode off: %v", err)
	}
	if _, ok := mode.Current(ctl.sess.Log.Events()); ok {
		t.Error("mode still active after /mode off")
	}
	if hist := mode.History(ctl.sess.Log.Events()); len(hist) != 1 || hist[0].Phase != "verify" {
		t.Errorf("history after exit = %+v, want one instance ending at verify", hist)
	}

	// /phase with no active mode is an error, not a silent no-op.
	if _, err := ctl.cmdPhase(ctx, []string{"next"}); err == nil {
		t.Error("cmdPhase ran with no active mode")
	}
}

// phaseSkillBlocks returns the auto-loaded process-skill blocks (AS-074) on the
// controller's log, by attributed skill name.
func phaseSkillBlocks(ctl *chatSession) []string {
	var names []string
	for _, b := range ctl.sess.Log.Events() {
		if b.Provenance != nil && b.Provenance.Producer == phaseSkillProducer && b.Attribution != nil {
			names = append(names, b.Attribution.Skill)
		}
	}
	return names
}

// TestPhaseSkillsAutoLoadPerPhase covers AS-074: the bundled process skills are
// auto-loaded per phase while Coding Mode is active, dedupe across re-entry, and
// add nothing when the mode is off.
func TestPhaseSkillsAutoLoadPerPhase(t *testing.T) {
	ctl := newTestController(t)
	ctx := context.Background()

	// Zero cost when off: no mode, no process-skill blocks.
	if got := phaseSkillBlocks(ctl); len(got) != 0 {
		t.Fatalf("phase skills with no mode = %v, want none", got)
	}

	// Enter at "think", which declares no skills.
	if _, err := ctl.cmdFeature(ctx, []string{"add", "OAuth"}); err != nil {
		t.Fatalf("cmdFeature: %v", err)
	}
	if got := phaseSkillBlocks(ctl); len(got) != 0 {
		t.Fatalf("phase skills at think = %v, want none", got)
	}

	// analyse auto-loads its two skills, into model-facing context.
	if _, err := ctl.cmdPhase(ctx, []string{"analyse"}); err != nil {
		t.Fatalf("cmdPhase analyse: %v", err)
	}
	if got := phaseSkillBlocks(ctl); len(got) != 2 {
		t.Fatalf("phase skills at analyse = %v, want grill-gaps + find-side-effects", got)
	}
	live := projection.Project(ctl.sess.Log.Events(), projection.Options{}).Live()
	var loadedBodies int
	for _, b := range live {
		if b.Provenance != nil && b.Provenance.Producer == phaseSkillProducer {
			loadedBodies++
		}
	}
	if loadedBodies != 2 {
		t.Errorf("process-skill blocks in live context = %d, want 2", loadedBodies)
	}

	// Jump away and back: no duplicate injection for analyse.
	if _, err := ctl.cmdPhase(ctx, []string{"plan"}); err != nil {
		t.Fatalf("cmdPhase plan: %v", err)
	}
	if _, err := ctl.cmdPhase(ctx, []string{"analyse"}); err != nil {
		t.Fatalf("cmdPhase analyse again: %v", err)
	}
	got := phaseSkillBlocks(ctl)
	// analyse: 2 (not 4), plan: 1.
	count := map[string]int{}
	for _, n := range got {
		count[n]++
	}
	if count["grill-gaps"] != 1 || count["find-side-effects"] != 1 || count["plan-review"] != 1 {
		t.Errorf("auto-load counts = %v, want each skill loaded exactly once", count)
	}
}

// TestPhaseSkillOverrideAndDisable covers AS-074's per-project swappability: a
// user/project skill of the same name replaces the bundled one, and an empty body
// disables it for the phase — without breaking the mode shell (AS-075).
func TestPhaseSkillOverrideAndDisable(t *testing.T) {
	ctl := newTestController(t)
	ctl.skills = []skill.Skill{
		{Name: "grill-gaps", Body: ""},                    // disable
		{Name: "find-side-effects", Body: "PROJECT BODY"}, // replace
	}
	ctx := context.Background()

	if _, err := ctl.cmdFeature(ctx, []string{"x"}); err != nil {
		t.Fatalf("cmdFeature: %v", err)
	}
	if _, err := ctl.cmdPhase(ctx, []string{"analyse"}); err != nil {
		t.Fatalf("cmdPhase analyse: %v", err)
	}

	var bodies = map[string]string{}
	for _, b := range ctl.sess.Log.Events() {
		if b.Provenance != nil && b.Provenance.Producer == phaseSkillProducer && b.Attribution != nil && b.Text != nil {
			bodies[b.Attribution.Skill] = b.Text.Text
		}
	}
	if _, disabled := bodies["grill-gaps"]; disabled {
		t.Errorf("grill-gaps was disabled (empty body) but a block was loaded")
	}
	if bodies["find-side-effects"] != "PROJECT BODY" {
		t.Errorf("find-side-effects body = %q, want the project override", bodies["find-side-effects"])
	}
}

func TestCmdBudget(t *testing.T) {
	ctl := newTestController(t)

	// No budget yet.
	out, err := ctl.cmdBudget(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdBudget show: %v", err)
	}
	if !strings.Contains(out.Text, "No budget set") {
		t.Errorf("unset /budget = %q, want a 'No budget set' notice", out.Text)
	}

	// Set a ceiling; it is recorded on the log.
	if _, err := ctl.cmdBudget(context.Background(), []string{"$0.50"}); err != nil {
		t.Fatalf("cmdBudget set: %v", err)
	}
	if limit, ok := budget.Current(ctl.sess.Log.Events()); !ok || limit != 0.50 {
		t.Errorf("after set, log budget = (%v, %v), want (0.50, true)", limit, ok)
	}

	// Show now reports the ceiling and warning threshold.
	out, err = ctl.cmdBudget(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdBudget show set: %v", err)
	}
	if !strings.Contains(out.Text, "$0.50") || !strings.Contains(out.Text, "warn at") {
		t.Errorf("set /budget show = %q, want ceiling + warn threshold", out.Text)
	}

	// Clear it.
	if _, err := ctl.cmdBudget(context.Background(), []string{"off"}); err != nil {
		t.Fatalf("cmdBudget off: %v", err)
	}
	if limit, ok := budget.Current(ctl.sess.Log.Events()); !ok || limit != 0 {
		t.Errorf("after off, log budget = (%v, %v), want (0, true)", limit, ok)
	}

	// A non-numeric amount is rejected.
	if _, err := ctl.cmdBudget(context.Background(), []string{"lots"}); err == nil {
		t.Error("cmdBudget accepted a non-numeric amount")
	}
}
