package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/manifest"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/schema"
)

func tokp(n int) *int { return &n }

// seedReplaySession isolates HOME and the working directory, then creates a
// session on disk with a small transcript and a usage event, so replayRun reads a
// real persisted log. It returns the session id.
func seedReplaySession(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	store, err := session.NewStore("", ".")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	sess, err := store.Create("")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	blocks := []schema.Block{
		{ID: schema.NewID(), Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "list the files"}},
		{ID: schema.NewID(), Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{ToolUseID: schema.NewID(), Name: "shell"}},
		{ID: schema.NewID(), Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{ToolUseID: schema.NewID(), Stdout: "README.md"}},
		{ID: schema.NewID(), Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: "there is one file"}},
		{ID: schema.NewID(), Kind: eventlog.KindUsage, Role: schema.RoleAssistant, Provider: &schema.Provider{Model: "model-a"}, Tokens: &schema.Tokens{Input: tokp(120), Output: tokp(15)}},
	}
	for _, b := range blocks {
		if _, err := sess.Log.Append(b); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := sess.Log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return sess.ID
}

func TestReplayRendersTranscriptAndManifest(t *testing.T) {
	id := seedReplaySession(t)

	var out, errb bytes.Buffer
	ctx := &cli.Context{Args: []string{id}, Stdout: &out, Stderr: &errb}
	if err := replayRun(ctx, false); err != nil {
		t.Fatalf("replay: %v", err)
	}
	got := out.String()
	for _, want := range []string{"re-display", "model-a", "list the files", "→ shell", "← README.md", "there is one file"} {
		if !strings.Contains(got, want) {
			t.Errorf("replay output missing %q\n---\n%s", want, got)
		}
	}
}

func TestReplayWritesManifestForUnmanifestedSession(t *testing.T) {
	id := seedReplaySession(t)

	store, err := session.NewStore("", ".")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	sess, err := store.Open(id)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dir := sess.Dir
	_ = sess.Log.Close()

	if _, ok, _ := manifest.Read(dir); ok {
		t.Fatal("precondition: session should have no manifest before replay")
	}

	var out, errb bytes.Buffer
	ctx := &cli.Context{Args: []string{id}, Stdout: &out, Stderr: &errb}
	if err := replayRun(ctx, false); err != nil {
		t.Fatalf("replay: %v", err)
	}
	m, ok, err := manifest.Read(dir)
	if err != nil || !ok {
		t.Fatalf("manifest not written by replay: ok=%v err=%v", ok, err)
	}
	if m.SessionID != id || m.Turns != 1 {
		t.Errorf("rebuilt manifest wrong: %+v", m)
	}
}

func TestReplayJSONOutput(t *testing.T) {
	id := seedReplaySession(t)

	var out, errb bytes.Buffer
	ctx := &cli.Context{Args: []string{id}, Stdout: &out, Stderr: &errb}
	ctx.Globals.Output = cli.OutputJSON
	if err := replayRun(ctx, false); err != nil {
		t.Fatalf("replay json: %v", err)
	}
	var payload struct {
		Manifest manifest.Manifest `json:"manifest"`
		Blocks   []json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal replay json: %v\n%s", err, out.String())
	}
	if payload.Manifest.SessionID != id {
		t.Errorf("json manifest session = %q, want %q", payload.Manifest.SessionID, id)
	}
	if len(payload.Blocks) == 0 {
		t.Error("json replay has no blocks")
	}
}

func TestReplayOTelExportsToConfiguredCollector(t *testing.T) {
	id := seedReplaySession(t) // isolates HOME/cwd to a temp dir

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Configure the OTLP endpoint via the project config the run resolves.
	if err := os.MkdirAll(".smith", 0o755); err != nil {
		t.Fatalf("mkdir .smith: %v", err)
	}
	cfgJSON := `{"telemetry":{"otel_endpoint":"` + srv.URL + `"}}`
	if err := os.WriteFile(".smith/config.json", []byte(cfgJSON), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errb bytes.Buffer
	ctx := &cli.Context{Args: []string{id}, Stdout: &out, Stderr: &errb}
	if err := replayRun(ctx, true); err != nil {
		t.Fatalf("replay --otel: %v", err)
	}
	if !strings.Contains(string(gotBody), "\"traceId\"") {
		t.Fatalf("collector did not receive a trace; body=%q stderr=%q", gotBody, errb.String())
	}
}

// TestExportTelemetryNilConfigNoPanic guards the Gemini-flagged nil-config path:
// a failed config load leaves cfg nil, which must be a safe no-op, not a panic.
func TestExportTelemetryNilConfigNoPanic(t *testing.T) {
	id := seedReplaySession(t)
	store, err := session.NewStore("", ".")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	sess, err := store.Open(id)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Log.Close() }()
	// Must not panic and must export nothing (no endpoint without config).
	exportTelemetry(context.Background(), sess, nil, nil, io.Discard)
}

func TestReplayUnknownSessionErrors(t *testing.T) {
	seedReplaySession(t) // isolates HOME/cwd

	var out, errb bytes.Buffer
	ctx := &cli.Context{Args: []string{"sess_does_not_exist"}, Stdout: &out, Stderr: &errb}
	if err := replayRun(ctx, false); err == nil {
		t.Error("want error replaying an unknown session id")
	}
}
