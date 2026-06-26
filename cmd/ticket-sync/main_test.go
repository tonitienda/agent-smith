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

func TestPayloadIncludesTypeLabel(t *testing.T) {
	tc := &ticket{
		id:       "AS-999",
		title:    "Example",
		status:   "ready-to-implement",
		kind:     "bug",
		area:     "faces",
		priority: "P2",
	}

	raw, err := payload(tc, payloadOptions{})
	if err != nil {
		t.Fatalf("payload returned error: %v", err)
	}

	got := unmarshalPayload(t, raw)
	labels, ok := got["labels"].([]any)
	if !ok {
		t.Fatalf("labels = %T, want []any", got["labels"])
	}
	want := map[string]bool{
		"ready-to-implement": true,
		"area:faces":         true,
		"type:bug":           true,
		"P2":                 true,
	}
	for _, label := range labels {
		if s, ok := label.(string); ok {
			delete(want, s)
		}
	}
	if len(want) > 0 {
		t.Fatalf("labels missing %v from %v", want, labels)
	}
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

func TestSyncTicketReusesExistingIssueBeforeCreating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AS-999-example.md")
	if err := os.WriteFile(path, []byte("---\nid: AS-999\ngithub_issue: null\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := &ticket{
		path:     path,
		id:       "AS-999",
		title:    "Example",
		status:   "done",
		area:     "foundation",
		priority: "P0",
		body:     "# AS-999 · Example\n",
	}

	var calls []string
	restore := stubGHAPI(t, func(endpoint, method string, input []byte) ([]byte, error) {
		calls = append(calls, method+" "+endpoint)
		switch {
		case method == "GET" && strings.HasPrefix(endpoint, "search/issues?"):
			return []byte(`{"items":[{"number":321,"title":"[AS-999] Example"}]}`), nil
		case method == "PATCH" && endpoint == "repos/owner/repo/issues/321":
			got := unmarshalPayload(t, input)
			if got["state_reason"] != "completed" {
				t.Fatalf("state_reason = %v, want completed", got["state_reason"])
			}
			return []byte(`{}`), nil
		default:
			t.Fatalf("unexpected gh call: %s %s", method, endpoint)
			return nil, nil
		}
	})
	defer restore()

	if err := syncTicket("owner/repo", tc, syncOptions{}); err != nil {
		t.Fatalf("syncTicket returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "github_issue: 321") {
		t.Fatalf("ticket file was not linked to existing issue: %s", raw)
	}
	if got, want := strings.Join(calls, "\n"), "GET search/issues?q=AS-999+in%3Atitle+repo%3Aowner%2Frepo+type%3Aissue\nPATCH repos/owner/repo/issues/321"; got != want {
		t.Fatalf("calls =\n%s\nwant\n%s", got, want)
	}
}

func TestFindExistingIssueIgnoresLooseTitleMatches(t *testing.T) {
	restore := stubGHAPI(t, func(endpoint, method string, input []byte) ([]byte, error) {
		return []byte(`{"items":[{"number":1,"title":"AS-999 loose mention"},{"number":2,"title":"[AS-999] Example"}]}`), nil
	})
	defer restore()

	got, err := findExistingIssue("owner/repo", &ticket{id: "AS-999"})
	if err != nil {
		t.Fatalf("findExistingIssue returned error: %v", err)
	}
	if got != 2 {
		t.Fatalf("issue = %d, want 2", got)
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

func TestParseTicketReadsType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AS-999-example.md")
	content := `---
id: AS-999
title: "Example"
status: ready-to-implement
type: bug
github_issue: null
area: faces
priority: P2
---

# Body
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ticket, err := parseTicket(path)
	if err != nil {
		t.Fatalf("parseTicket returned error: %v", err)
	}
	if ticket.kind != "bug" {
		t.Fatalf("kind = %q, want bug", ticket.kind)
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

func stubGHAPI(t *testing.T, fn func(endpoint, method string, input []byte) ([]byte, error)) func() {
	t.Helper()
	old := ghAPI
	ghAPI = fn
	return func() {
		ghAPI = old
	}
}

func TestFilterTicketsAcceptsImplementationAndQACategories(t *testing.T) {
	got := filterTickets([]string{
		"docs/project/tickets/AS-123-example.md",
		"docs/project/tickets/AS-Q-123-qa-example.md",
		"docs/project/tickets/NOT-123-example.md",
	})
	want := "docs/project/tickets/AS-123-example.md\ndocs/project/tickets/AS-Q-123-qa-example.md"
	if strings.Join(got, "\n") != want {
		t.Fatalf("filterTickets = %q, want %q", strings.Join(got, "\n"), want)
	}
}

func TestSplitTicketIDSupportsQACategory(t *testing.T) {
	prefix, n, ok := splitTicketID("AS-Q-123")
	if !ok || prefix != "AS-Q" || n != 123 {
		t.Fatalf("splitTicketID(AS-Q-123) = %q, %d, %v; want AS-Q, 123, true", prefix, n, ok)
	}
}
