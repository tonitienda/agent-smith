package main

import (
	"context"
	"strings"
	"testing"
)

// TestCmdStats asserts /stats renders the portfolio header over the project's
// sessions and routes the optional scope arg (AS-057).
func TestCmdStats(t *testing.T) {
	ctl := newTestController(t)

	out, err := ctl.cmdStats(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdStats: %v", err)
	}
	for _, want := range []string{"Cross-session analytics — this project", "session"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("report missing %q:\n%s", want, out.Text)
		}
	}

	all, err := ctl.cmdStats(context.Background(), []string{"all"})
	if err != nil {
		t.Fatalf("cmdStats all: %v", err)
	}
	if !strings.Contains(all.Text, "all projects") {
		t.Fatalf("all-scope report missing scope label:\n%s", all.Text)
	}
}

// TestCmdStatsRejectsUnknownArg keeps the arg contract tight: only `all` widens
// the scope, anything else is a usage hint, not a silent no-op.
func TestCmdStatsRejectsUnknownArg(t *testing.T) {
	ctl := newTestController(t)
	out, err := ctl.cmdStats(context.Background(), []string{"bogus"})
	if err != nil {
		t.Fatalf("cmdStats: %v", err)
	}
	if !strings.Contains(out.Text, "Usage:") {
		t.Fatalf("expected usage hint, got:\n%s", out.Text)
	}
}
