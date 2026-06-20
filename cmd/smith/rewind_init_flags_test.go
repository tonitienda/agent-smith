package main

import (
	"strings"
	"testing"

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
