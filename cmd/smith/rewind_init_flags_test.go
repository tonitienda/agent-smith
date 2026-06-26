package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/initscaffold"
	"github.com/tonitienda/agent-smith/internal/rewind"
)

// TestRewindMarkParsesStringFlag is the AS-105 string-flag path: /rewind --mark
// "<label>" carries the label through the shared flag contract (a flag.String,
// permuted with its value) — not by reading args past the flag — and records a
// named checkpoint the picker can return to.
func TestRewindMarkParsesStringFlag(t *testing.T) {
	ctl := newTestController(t)
	appendUserText(t, ctl, "a turn to anchor the mark after")

	out, err := runChatCommand(t, ctl, "rewind", "--mark", "before refactor")
	if err != nil {
		t.Fatalf(`/rewind --mark "before refactor": %v`, err)
	}
	if !strings.Contains(out.Text, "before refactor") {
		t.Errorf("mark confirmation = %q, want it to name the label", out.Text)
	}

	cps := rewind.Checkpoints(ctl.sess.Log.Events())
	var found bool
	for _, c := range cps {
		if c.Label == "before refactor" {
			found = true
		}
	}
	if !found {
		t.Errorf("no checkpoint labeled %q recorded; checkpoints=%+v", "before refactor", cps)
	}
}

// TestRewindMarkEmptyLabelAsksForOne confirms --mark with no label is still
// "mark requested" (Flags.Set), so the handler explains the label is required
// rather than falling through to the checkpoint list.
func TestRewindMarkEmptyLabelAsksForOne(t *testing.T) {
	ctl := newTestController(t)
	out, err := runChatCommand(t, ctl, "rewind", "--mark", "")
	if err != nil {
		t.Fatalf(`/rewind --mark "": %v`, err)
	}
	if !strings.Contains(out.Text, "label") {
		t.Errorf("empty --mark = %q, want a prompt for a label", out.Text)
	}
}

// TestRewindUndeclaredFlagIsUsageError covers the AS-105 acceptance criterion
// that an undeclared flag fails at parse time rather than being hand-matched.
func TestRewindUndeclaredFlagIsUsageError(t *testing.T) {
	ctl := newTestController(t)
	if _, err := runChatCommand(t, ctl, "rewind", "--bogus"); err == nil {
		t.Error("/rewind --bogus: want a usage error, got nil")
	}
}

// TestInitFlagsDispatchThroughContract covers /init on the shared contract:
// --apply/--cancel reach the handler via FlagsFrom(ctx), and an undeclared flag
// is a usage error.
func TestInitFlagsDispatchThroughContract(t *testing.T) {
	ctl := newTestController(t)

	if _, err := runChatCommand(t, ctl, "init", "--cancel"); err != nil {
		t.Fatalf("/init --cancel: %v", err)
	}
	if _, err := runChatCommand(t, ctl, "init", "--bogus"); err == nil {
		t.Error("/init --bogus: want a usage error, got nil")
	}
}

// stubEnricher returns a fixed prose section, standing in for the model-backed
// enricher so the --describe dispatch can be tested offline.
type stubEnricher struct{}

func (stubEnricher) Enrich(context.Context, initscaffold.Facts) ([]initscaffold.ProseSection, error) {
	return []initscaffold.ProseSection{{Title: "Overview", Body: "A widget service."}}, nil
}

// AS-087: /init --describe runs the enrichment pass, so the staged preview gains
// the model-authored prose section on top of the deterministic scaffold. Plain
// /init does not invoke the enricher.
func TestInitDescribeAddsProse(t *testing.T) {
	ctl := newTestController(t)
	if err := os.WriteFile(filepath.Join(ctl.wd, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctl.setInitEnricher(stubEnricher{})

	out, err := runChatCommand(t, ctl, "init", "--describe")
	if err != nil {
		t.Fatalf("/init --describe: %v", err)
	}
	if !strings.Contains(out.Text, "## Overview") || !strings.Contains(out.Text, "A widget service.") {
		t.Errorf("--describe preview missing prose:\n%s", out.Text)
	}

	plain, err := runChatCommand(t, ctl, "init")
	if err != nil {
		t.Fatalf("/init: %v", err)
	}
	if strings.Contains(plain.Text, "## Overview") {
		t.Errorf("plain /init should not invoke the enricher:\n%s", plain.Text)
	}
}

// AS-087: --describe with no enricher wired (no provider configured) says so
// rather than silently degrading to the deterministic scan.
func TestInitDescribeNoProviderNotes(t *testing.T) {
	ctl := newTestController(t) // no enricher wired
	out, err := runChatCommand(t, ctl, "init", "--describe")
	if err != nil {
		t.Fatalf("/init --describe: %v", err)
	}
	if !strings.Contains(out.Text, "no active provider configured") {
		t.Errorf("--describe with no enricher should note the skip:\n%s", out.Text)
	}
}
