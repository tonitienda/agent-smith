package main

import (
	"context"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/compact"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

func intp(n int) *int { return &n }

// newCompactController builds a controller whose "anthropic" vendor is a mock
// that scripts a one-line summary turn with usage, so /compact --apply can run
// without a live API. The active model is the Anthropic default, so cheapModel
// stays on the anthropic vendor and resolves to the mock.
func newCompactController(t *testing.T) (*chatSession, *provider.Mock) {
	t.Helper()
	store, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	sess, err := store.Create("")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	mock := &provider.Mock{
		NameValue: "anthropic",
		Events: []provider.Event{
			{Type: provider.EventTextDelta, TextDelta: "Earlier the user asked X and the assistant did Y."},
			{Type: provider.EventUsage, Usage: &schema.Tokens{Input: intp(120), Output: intp(18)}},
			{Type: provider.EventTurnStop, StopReason: provider.StopEndTurn},
		},
	}
	providers := map[string]provider.Provider{"anthropic": mock}
	ctl := newChatSession(store, tool.NewRegistry(), cost.Embedded(), providers, sess, "anthropic", "claude-opus-4-8", t.TempDir(), nil, nil)
	if err := ctl.start(func(loop.UIEvent) {}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return ctl, mock
}

func appendBlock(t *testing.T, ctl *chatSession, b schema.Block) {
	t.Helper()
	if b.ID == "" {
		b.ID = schema.NewID()
	}
	if _, err := ctl.sess.Log.Append(b); err != nil {
		t.Fatalf("append: %v", err)
	}
}

// seedConversation appends a system prefix, an old turn, and a recent user turn,
// so the old turn is the compactable span and the recent turn is kept.
func seedConversation(t *testing.T, ctl *chatSession) {
	t.Helper()
	appendBlock(t, ctl, schema.Block{ID: "blk_sys00000", Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: "be terse"}})
	appendBlock(t, ctl, schema.Block{ID: "blk_old0user", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: strings.Repeat("q", 200)}})
	appendBlock(t, ctl, schema.Block{ID: "blk_old0asst", Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: strings.Repeat("a", 400)}})
	appendBlock(t, ctl, schema.Block{ID: "blk_new0user", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "the latest question"}})
}

// TestCompactApplyShrinksAndItemizesCost covers the apply path end to end:
// summarization runs on the cheap tier, the sources leave the window, the
// summary lands as a compaction block, and the cheap-tier usage is recorded so
// /cost itemizes it (AS-038 AC1, AC4).
func TestCompactApplyShrinksAndItemizesCost(t *testing.T) {
	ctl, mock := newCompactController(t)
	seedConversation(t, ctl)

	if out, err := ctl.cmdCompact(context.TODO(), nil); err != nil {
		t.Fatalf("preview: %v", err)
	} else if !strings.Contains(out.Text, "reclaim") {
		t.Fatalf("preview did not offer a reclaim figure: %q", out.Text)
	}

	out, err := ctl.cmdCompact(context.TODO(), []string{"--apply"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(out.Text, "Compacted") {
		t.Errorf("apply text = %q, want a 'Compacted' confirmation", out.Text)
	}

	// The cheap-tier model was asked to summarize.
	if len(mock.Requests()) != 1 || mock.Requests()[0].Model != "claude-haiku-4-5" {
		t.Errorf("summarization model = %+v, want one request on claude-haiku-4-5", mock.Requests())
	}

	// Sources left the window; the summary is live.
	if liveContains(t, ctl, "blk_old0user") || liveContains(t, ctl, "blk_old0asst") {
		t.Error("a compacted source is still live")
	}
	if !liveContains(t, ctl, "blk_new0user") {
		t.Error("the recent turn was compacted away")
	}

	// A /compact usage event was recorded for cost itemization.
	if !hasCompactUsage(ctl) {
		t.Error("no /compact usage event recorded; /cost would not itemize the summarization")
	}
	summary := cost.Summarize(ctl.sess.Log.Events(), ctl.pricing)
	if summary.TotalUSD <= 0 {
		t.Errorf("expected a non-zero session cost after compaction, got %v", summary.TotalUSD)
	}
}

// TestCompactUndoRestores covers AC2: undo restores the exact prior live set.
func TestCompactUndoRestores(t *testing.T) {
	ctl, _ := newCompactController(t)
	seedConversation(t, ctl)

	if _, err := ctl.cmdCompact(context.TODO(), nil); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := ctl.cmdCompact(context.TODO(), []string{"--apply"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := ctl.cmdCompact(context.TODO(), []string{"--undo"}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	for _, id := range []string{"blk_old0user", "blk_old0asst", "blk_new0user"} {
		if !liveContains(t, ctl, id) {
			t.Errorf("block %s not restored after undo", id)
		}
	}
}

// TestCompactCancelLeavesLog confirms --cancel discards the stage without writing.
func TestCompactCancelLeavesLog(t *testing.T) {
	ctl, _ := newCompactController(t)
	seedConversation(t, ctl)
	before := len(ctl.sess.Log.Events())

	if _, err := ctl.cmdCompact(context.TODO(), nil); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := ctl.cmdCompact(context.TODO(), []string{"--cancel"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := len(ctl.sess.Log.Events()); got != before {
		t.Errorf("log grew from %d to %d on a cancelled compaction", before, got)
	}
}

func hasCompactUsage(ctl *chatSession) bool {
	for _, b := range ctl.sess.Log.Events() {
		if b.Kind == eventlog.KindUsage && b.Provenance != nil && b.Provenance.Producer == compact.Producer {
			return true
		}
	}
	return false
}

func usageProducer(ctl *chatSession, producer string) bool {
	for _, b := range ctl.sess.Log.Events() {
		if b.Kind == eventlog.KindUsage && b.Provenance != nil && b.Provenance.Producer == producer {
			return true
		}
	}
	return false
}

// TestAutoCompactDisabledByDefault is AS-085 AC1: with the flag off, a turn's
// pre-turn hook compacts nothing.
func TestAutoCompactDisabledByDefault(t *testing.T) {
	ctl, _ := newCompactController(t)
	seedConversation(t, ctl)
	before := len(ctl.sess.Log.Events())

	ctl.maybeAutoCompact(context.TODO())

	if got := len(ctl.sess.Log.Events()); got != before {
		t.Errorf("log grew from %d to %d with auto-compact off", before, got)
	}
}

// TestAutoCompactBelowThresholdNoop: enabled but the window is well under the
// threshold, so nothing compacts.
func TestAutoCompactBelowThresholdNoop(t *testing.T) {
	ctl, _ := newCompactController(t)
	seedConversation(t, ctl)
	ctl.setAutoCompact(true, 0.99) // ~200k-token window; the seeded context is tiny
	before := len(ctl.sess.Log.Events())

	ctl.maybeAutoCompact(context.TODO())

	if got := len(ctl.sess.Log.Events()); got != before {
		t.Errorf("log grew from %d to %d below the threshold", before, got)
	}
}

// TestAutoCompactTriggersOnThreshold is AS-085 AC2/AC3/AC4: crossing the
// threshold compacts the older span before the turn, the result is reversible
// (/compact --undo), it is surfaced (UIAutoCompact, never silent), and its
// summarization cost is itemized under the distinct auto producer.
func TestAutoCompactTriggersOnThreshold(t *testing.T) {
	ctl, mock := newCompactController(t)
	var notices []string
	ctl.observer = func(ev loop.UIEvent) {
		if ev.Kind == loop.UIAutoCompact {
			notices = append(notices, ev.Text)
		}
	}
	seedConversation(t, ctl)
	ctl.setAutoCompact(true, 0.0001) // a tiny threshold the seeded context clears

	ctl.maybeAutoCompact(context.TODO())

	// The cheap tier summarized.
	if len(mock.Requests()) != 1 || mock.Requests()[0].Model != "claude-haiku-4-5" {
		t.Errorf("summarization requests = %+v, want one on claude-haiku-4-5", mock.Requests())
	}
	// Older sources left the window; the recent turn is kept.
	if liveContains(t, ctl, "blk_old0user") || liveContains(t, ctl, "blk_old0asst") {
		t.Error("an auto-compacted source is still live")
	}
	if !liveContains(t, ctl, "blk_new0user") {
		t.Error("the recent turn was auto-compacted away")
	}
	// Cost itemized under the distinct auto producer (not the manual /compact one).
	if !usageProducer(ctl, compact.AutoUsageProducer) {
		t.Error("no auto-compact usage event recorded for /cost itemization")
	}
	if hasCompactUsage(ctl) {
		t.Error("auto-compaction recorded manual /compact usage; producers must stay distinct")
	}
	// Surfaced, never silent (D0).
	if len(notices) == 0 {
		t.Error("auto-compaction emitted no UIAutoCompact notice")
	}
	// Reversible via the same /compact --undo path.
	if _, err := ctl.cmdCompact(context.TODO(), []string{"--undo"}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	for _, id := range []string{"blk_old0user", "blk_old0asst", "blk_new0user"} {
		if !liveContains(t, ctl, id) {
			t.Errorf("block %s not restored after undoing auto-compaction", id)
		}
	}
}
