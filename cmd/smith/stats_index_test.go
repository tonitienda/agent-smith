package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// appendUsage writes a priced provider turn to the controller's current session
// so cross-session analytics has spend to aggregate.
func appendUsage(t *testing.T, ctl *chatSession, model string, in, out int) {
	t.Helper()
	if _, err := ctl.sess.Log.Append(eventlog.NewUsage(
		"loop", "anthropic", model, "end_turn",
		&schema.Tokens{Input: intp(in), Output: intp(out)}, nil,
	)); err != nil {
		t.Fatalf("append usage: %v", err)
	}
}

// TestCmdStatsWritesIndex asserts a `stats` call persists the derived index under
// the state root (AS-136), so a later call can read pre-priced rows.
func TestCmdStatsWritesIndex(t *testing.T) {
	ctl := newTestController(t)
	appendUsage(t, ctl, "claude-opus-4-8", 1000, 500)

	if _, err := ctl.cmdStats(context.Background(), nil); err != nil {
		t.Fatalf("cmdStats: %v", err)
	}
	idxPath := filepath.Join(ctl.store.Root(), statsIndexName)
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("expected stats index at %s: %v", idxPath, err)
	}
}

// TestCmdStatsRebuildMatchesFresh asserts the index is disposable (AS-136 AC):
// deleting it and re-running `stats` yields the same report a rebuild produces.
func TestCmdStatsRebuildMatchesFresh(t *testing.T) {
	ctl := newTestController(t)
	appendUsage(t, ctl, "claude-opus-4-8", 2000, 800)

	first, err := ctl.cmdStats(context.Background(), []string{"all"})
	if err != nil {
		t.Fatalf("cmdStats all: %v", err)
	}

	// Rebuild from logs, then delete the index so the next read must recompute.
	if out, err := ctl.cmdStats(context.Background(), []string{"rebuild"}); err != nil {
		t.Fatalf("cmdStats rebuild: %v", err)
	} else if !strings.Contains(out.Text, "Rebuilt stats index") {
		t.Fatalf("rebuild confirmation missing:\n%s", out.Text)
	}
	if err := os.Remove(filepath.Join(ctl.store.Root(), statsIndexName)); err != nil {
		t.Fatalf("remove index: %v", err)
	}

	recomputed, err := ctl.cmdStats(context.Background(), []string{"all"})
	if err != nil {
		t.Fatalf("cmdStats all (no index): %v", err)
	}
	if recomputed.Text != first.Text {
		t.Fatalf("report differs with/without index:\n--- indexed ---\n%s\n--- recomputed ---\n%s", first.Text, recomputed.Text)
	}
}

// TestCmdStatsCrossProjectFriction asserts `stats all` merges per-project findings
// logs so a fact recurring across projects surfaces as friction (AS-136 AC).
func TestCmdStatsCrossProjectFriction(t *testing.T) {
	ctl := newTestController(t)
	appendUsage(t, ctl, "claude-opus-4-8", 100, 50)

	root := ctl.store.Root()
	// Same finding signature raised in two distinct sessions under two different
	// projects — recurring only when the project logs are merged.
	writeFindings(t, filepath.Join(root, "sessions", "projA", skillFindingsName),
		`{"session":"s1","kind":"fact","summary":"re-explained the build layout"}`)
	writeFindings(t, filepath.Join(root, "sessions", "projB", skillFindingsName),
		`{"session":"s2","kind":"fact","summary":"re-explained the build layout"}`)

	out, err := ctl.cmdStats(context.Background(), []string{"all"})
	if err != nil {
		t.Fatalf("cmdStats all: %v", err)
	}
	if !strings.Contains(out.Text, "re-explained the build layout") {
		t.Fatalf("cross-project friction missing:\n%s", out.Text)
	}
}

func writeFindings(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write findings: %v", err)
	}
}
