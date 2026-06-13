package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdatePayloadClosesDoneTickets(t *testing.T) {
	tc := &ticket{
		path:     "docs/project/tickets/AS-999-example.md",
		id:       "AS-999",
		title:    "Example",
		status:   "done",
		area:     "foundation",
		priority: "P0",
		body:     "# AS-999 · Example\n",
	}

	raw, err := payload(tc, payloadOptions{includeState: true})
	if err != nil {
		t.Fatalf("payload returned error: %v", err)
	}

	got := unmarshalPayload(t, raw)
	if got["state"] != "closed" {
		t.Fatalf("state = %v, want closed", got["state"])
	}
	if got["state_reason"] != "completed" {
		t.Fatalf("state_reason = %v, want completed", got["state_reason"])
	}
	if got["title"] != "[AS-999] Example" {
		t.Fatalf("title = %v, want [AS-999] Example", got["title"])
	}
	if body, ok := got["body"].(string); !ok || !strings.Contains(body, "Synced from `docs/project/tickets/AS-999-example.md`") {
		t.Fatalf("body = %v, want sync footer", got["body"])
	}
}

func TestCreatePayloadLeavesDoneIssueStateUntouched(t *testing.T) {
	tc := &ticket{id: "AS-999", title: "Example", status: "done"}

	raw, err := payload(tc, payloadOptions{})
	if err != nil {
		t.Fatalf("payload returned error: %v", err)
	}

	got := unmarshalPayload(t, raw)
	assertNoState(t, got)
}

func TestUpdatePayloadLeavesNonDoneIssueStateUntouched(t *testing.T) {
	tc := &ticket{id: "AS-999", title: "Example", status: "ready-to-implement"}

	raw, err := payload(tc, payloadOptions{includeState: true})
	if err != nil {
		t.Fatalf("payload returned error: %v", err)
	}

	got := unmarshalPayload(t, raw)
	assertNoState(t, got)
}

func TestClosePayloadClosesIssueAsCompleted(t *testing.T) {
	raw, err := closePayload()
	if err != nil {
		t.Fatalf("closePayload returned error: %v", err)
	}

	got := unmarshalPayload(t, raw)
	if got["state"] != "closed" {
		t.Fatalf("state = %v, want closed", got["state"])
	}
	if got["state_reason"] != "completed" {
		t.Fatalf("state_reason = %v, want completed", got["state_reason"])
	}
}

func TestRequireExistingRejectsUnlinkedTicket(t *testing.T) {
	err := syncTicket("owner/repo", &ticket{id: "AS-999"}, syncOptions{dryRun: true, requireExisting: true})
	if err == nil {
		t.Fatal("syncTicket returned nil error, want require-existing failure")
	}
	if !strings.Contains(err.Error(), "github_issue is null") {
		t.Fatalf("error %q does not mention missing github_issue", err)
	}
}

func TestSkipUnlinkedLeavesUnlinkedTicketAlone(t *testing.T) {
	// dryRun is false to prove the skip happens before any GitHub call.
	if err := syncTicket("owner/repo", &ticket{id: "AS-999"}, syncOptions{skipUnlinked: true}); err != nil {
		t.Fatalf("syncTicket returned error, want unlinked ticket skipped: %v", err)
	}
}

func TestIsLinked(t *testing.T) {
	cases := map[string]bool{
		"---\nid: AS-1\ngithub_issue: 42\n---\n":   true,
		"---\nid: AS-1\ngithub_issue: null\n---\n": false,
		"---\nid: AS-1\ngithub_issue:\n---\n":      false,
		"---\nid: AS-1\n---\n":                     false,
	}
	for content, want := range cases {
		path := filepath.Join(t.TempDir(), "AS-1-x.md")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := isLinked(path); got != want {
			t.Fatalf("isLinked(%q) = %v, want %v", content, got, want)
		}
	}
}

func unmarshalPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload is invalid JSON: %v", err)
	}
	return got
}

func assertNoState(t *testing.T, got map[string]any) {
	t.Helper()
	if _, ok := got["state"]; ok {
		t.Fatalf("state was present: %v", got["state"])
	}
	if _, ok := got["state_reason"]; ok {
		t.Fatalf("state_reason was present: %v", got["state_reason"])
	}
}
