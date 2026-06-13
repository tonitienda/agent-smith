package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPayloadClosesDoneTickets(t *testing.T) {
	tc := &ticket{
		path:     "docs/project/tickets/AS-999-example.md",
		id:       "AS-999",
		title:    "Example",
		status:   "done",
		area:     "foundation",
		priority: "P0",
		body:     "# AS-999 · Example\n",
	}

	raw, err := payload(tc)
	if err != nil {
		t.Fatalf("payload returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload is invalid JSON: %v", err)
	}
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

func TestPayloadLeavesNonDoneIssueStateUntouched(t *testing.T) {
	tc := &ticket{id: "AS-999", title: "Example", status: "ready-to-implement"}

	raw, err := payload(tc)
	if err != nil {
		t.Fatalf("payload returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload is invalid JSON: %v", err)
	}
	if _, ok := got["state"]; ok {
		t.Fatalf("state was present for non-done ticket: %v", got["state"])
	}
	if _, ok := got["state_reason"]; ok {
		t.Fatalf("state_reason was present for non-done ticket: %v", got["state_reason"])
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
