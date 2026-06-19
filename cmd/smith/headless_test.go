package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
)

// useMockProvider points the headless run path at a mock anthropic provider for
// the duration of the test, so `smith run` exercises end-to-end without a network
// call or real key (AS-051 AC1). It also moves into a fresh working directory so
// the created session lands in an isolated .smith store.
func useMockProvider(t *testing.T, mock *provider.Mock) {
	t.Helper()
	t.Chdir(t.TempDir())
	prev := providersFn
	providersFn = func() map[string]provider.Provider {
		return map[string]provider.Provider{"anthropic": mock}
	}
	t.Cleanup(func() { providersFn = prev })
}

// TestRunOutputJSON covers AC1: a scripted run completes end-to-end and emits a
// parseable structured result carrying the answer, session id, and stop reason.
func TestRunOutputJSON(t *testing.T) {
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("the answer is 42", provider.StopEndTurn),
	})
	app, out, _ := testApp(false, false)

	if code := app.Run([]string{"run", "compute the answer", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("exit = %d, want %d; stdout=%s", code, cli.ExitOK, out.String())
	}
	var res runResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("parse JSON result: %v; raw=%s", err, out.String())
	}
	if res.Text != "the answer is 42" {
		t.Errorf("Text = %q, want the scripted answer", res.Text)
	}
	if res.StopReason != provider.StopEndTurn {
		t.Errorf("StopReason = %q, want %q", res.StopReason, provider.StopEndTurn)
	}
	if res.SessionID == "" {
		t.Error("SessionID is empty; a headless run must record a resumable session")
	}
}

// TestRunPermissionDeniedExits covers AC2 / D-CLI-9: with the default
// allowlist-then-deny posture a tool call is denied with a machine-readable
// reason and the run exits with the permission-stop class.
func TestRunPermissionDeniedExits(t *testing.T) {
	calls := 0
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		// First turn asks to run a shell command (denied off the allowlist); the
		// loop feeds the denial back and the second turn finishes with text.
		ScriptFn: func(context.Context, provider.Request) ([]provider.Event, error) {
			calls++
			if calls == 1 {
				return provider.ToolCallTurn("call_1", "shell", json.RawMessage(`{"command":"ls"}`)), nil
			}
			return provider.TextTurn("done without the tool", provider.StopEndTurn), nil
		},
	})
	app, out, _ := testApp(false, false)

	if code := app.Run([]string{"run", "list the files", "--output", "json"}); code != cli.ExitPermission {
		t.Fatalf("exit = %d, want %d (permission-stop); stdout=%s", code, cli.ExitPermission, out.String())
	}
	var res runResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("parse JSON result: %v; raw=%s", err, out.String())
	}
	if len(res.Denied) == 0 || res.Denied[0].Tool != "shell" {
		t.Fatalf("Denied = %+v, want a shell denial", res.Denied)
	}
	if res.Error == "" {
		t.Error("Error is empty; a non-OK run must report its reason")
	}
}

// TestRunBudgetStopExits covers AC3: a --budget below a turn's worst-case cost
// halts the run before spending, surfacing the budget stop reason and exit code
// with a (partial) structured result.
func TestRunBudgetStopExits(t *testing.T) {
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("should never run", provider.StopEndTurn),
	})
	app, out, _ := testApp(false, false)

	// A sub-cent ceiling is below any priced turn's worst-case reservation.
	if code := app.Run([]string{"run", "do a big task", "--budget", "0.0000001", "--output", "json"}); code != cli.ExitBudget {
		t.Fatalf("exit = %d, want %d (budget-stop); stdout=%s", code, cli.ExitBudget, out.String())
	}
	var res runResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("parse JSON result: %v; raw=%s", err, out.String())
	}
	if res.StopReason != "budget_exceeded" {
		t.Errorf("StopReason = %q, want budget_exceeded", res.StopReason)
	}
}

// TestRunResumable covers AC4: a headless session is a normal session, listed in
// the project store and resumable afterward.
func TestRunResumable(t *testing.T) {
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("hi", provider.StopEndTurn),
	})
	app, out, _ := testApp(false, false)
	if code := app.Run([]string{"run", "say hi", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("exit = %d, want %d", code, cli.ExitOK)
	}
	var res runResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	store, err := session.NewStore("", wd)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	summaries, err := store.List()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	var found bool
	for _, s := range summaries {
		if s.ID == res.SessionID {
			found = true
		}
	}
	if !found {
		t.Fatalf("session %q not found in the project store (%d sessions)", res.SessionID, len(summaries))
	}
}

// TestHeadlessPermissionAuto asserts the two postures D-CLI-9 defines: the default
// gate denies a call that would prompt (no allowlist, no asker), while --auto
// approves it for an unattended run.
func TestHeadlessPermissionAuto(t *testing.T) {
	t.Chdir(t.TempDir())
	shell := tool.Call{Name: "shell", Arguments: json.RawMessage(`{"command":"ls"}`)}

	deny, err := headlessPermission(".", "", false)
	if err != nil {
		t.Fatalf("build deny gate: %v", err)
	}
	if d := deny.decide(context.Background(), shell); d.Allow {
		t.Error("default posture allowed a shell call; want allowlist-then-deny")
	}
	if len(deny.denied()) != 1 {
		t.Errorf("denied() = %d, want 1 recorded denial", len(deny.denied()))
	}

	auto, err := headlessPermission(".", "", true)
	if err != nil {
		t.Fatalf("build auto gate: %v", err)
	}
	if d := auto.decide(context.Background(), shell); !d.Allow {
		t.Errorf("--auto denied a shell call: %q", d.Reason)
	}
}

// TestParseBudgetFlag checks the --budget parsing: empty is unmetered, a leading
// $ is tolerated, and a bad value is a usage error.
func TestParseBudgetFlag(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"", 0, false},
		{"0.25", 0.25, false},
		{"$1.50", 1.50, false},
		{"-1", 0, true},
		{"abc", 0, true},
	} {
		got, err := parseBudgetFlag(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseBudgetFlag(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parseBudgetFlag(%q) = %v, %v; want %v, nil", tc.in, got, err, tc.want)
		}
	}
}
