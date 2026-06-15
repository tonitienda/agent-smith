package main

import (
	"context"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/openai"
	"github.com/tonitienda/agent-smith/internal/session"
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
	ctl := newChatSession(store, tool.NewRegistry(), cost.Embedded(), providers, sess, "anthropic", "claude-opus-4-8")
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
