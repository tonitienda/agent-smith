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
