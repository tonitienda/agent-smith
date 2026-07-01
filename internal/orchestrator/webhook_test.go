package orchestrator

import (
	"testing"
	"time"
)

func TestNormalizeIssueLabeled(t *testing.T) {
	payload := `{
		"action": "labeled",
		"repository": {"full_name": "o/r"},
		"sender": {"login": "octocat"},
		"label": {"name": "implementation"},
		"issue": {"number": 42, "updated_at": "2026-07-01T10:00:00Z",
			"labels": [{"name": "implementation"}, {"name": "bug"}]}
	}`
	ev, ok, err := Normalize("issues", "d1", []byte(payload))
	if err != nil || !ok {
		t.Fatalf("normalize: ok=%v err=%v", ok, err)
	}
	want := GitHubEvent{
		DeliveryID: "d1", Kind: "github.issue_labeled", Repository: "o/r",
		Label: "implementation", Number: 42, Actor: "octocat",
		Labels:    []string{"implementation", "bug"},
		EventTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	assertEvent(t, ev, want)
}

func TestNormalizePRLabeledAndMerged(t *testing.T) {
	labeled := `{
		"action": "labeled",
		"repository": {"full_name": "o/r"},
		"sender": {"login": "dev"},
		"label": {"name": "ready"},
		"pull_request": {"number": 7, "base": {"ref": "main"}, "labels": [{"name": "ready"}]}
	}`
	ev, ok, err := Normalize("pull_request", "d2", []byte(labeled))
	if err != nil || !ok {
		t.Fatalf("pr labeled: ok=%v err=%v", ok, err)
	}
	if ev.Kind != "github.pr_labeled" || ev.Label != "ready" || ev.Number != 7 || ev.Base != "main" {
		t.Fatalf("pr labeled event: %+v", ev)
	}

	merged := `{
		"action": "closed",
		"repository": {"full_name": "o/r"},
		"sender": {"login": "dev"},
		"pull_request": {"number": 7, "merged": true, "base": {"ref": "main"}}
	}`
	ev, ok, err = Normalize("pull_request", "d3", []byte(merged))
	if err != nil || !ok {
		t.Fatalf("pr merged: ok=%v err=%v", ok, err)
	}
	if ev.Kind != "github.pr_merged" || ev.Base != "main" {
		t.Fatalf("pr merged event: %+v", ev)
	}
}

func TestNormalizeCommentCommand(t *testing.T) {
	cases := map[string]string{
		"/implement":           "implement",
		"/smith implement now": "implement",
		"  \n/retry":           "retry",
		"please /implement":    "", // command must start the first non-empty line
		"no command here":      "",
		"/":                    "",
	}
	for body, want := range cases {
		payload := `{
			"action": "created",
			"repository": {"full_name": "o/r"},
			"sender": {"login": "u"},
			"issue": {"number": 3},
			"comment": {"body": ` + jsonString(body) + `, "created_at": "2026-07-01T09:00:00Z", "user": {"login": "u"}}
		}`
		ev, ok, err := Normalize("issue_comment", "d", []byte(payload))
		if err != nil {
			t.Fatalf("body %q: err %v", body, err)
		}
		if want == "" {
			if ok {
				t.Fatalf("body %q: expected no trigger, got %+v", body, ev)
			}
			continue
		}
		if !ok || ev.Kind != "github.comment_command" || ev.Command != want || ev.Number != 3 {
			t.Fatalf("body %q: want command %q, got ok=%v %+v", body, want, ok, ev)
		}
	}
}

func TestNormalizeNonTriggerEventsAreDropped(t *testing.T) {
	drops := []struct{ eventType, payload string }{
		{"issues", `{"action": "opened", "issue": {"number": 1}}`},
		{"pull_request", `{"action": "closed", "pull_request": {"number": 1, "merged": false}}`},
		{"pull_request", `{"action": "synchronize", "pull_request": {"number": 1}}`},
		{"push", `{"ref": "refs/heads/main"}`},
		{"issues", `{"action": "labeled"}`}, // no issue object
	}
	for _, d := range drops {
		ev, ok, err := Normalize(d.eventType, "x", []byte(d.payload))
		if err != nil {
			t.Fatalf("%s: unexpected err %v", d.eventType, err)
		}
		if ok {
			t.Fatalf("%s: expected drop, got %+v", d.eventType, ev)
		}
	}
}

func TestNormalizeMalformedPayloadErrors(t *testing.T) {
	if _, _, err := Normalize("issues", "d", []byte("{not json")); err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

// TestNormalizeThenEnqueueIsIdempotent proves a re-delivered webhook (same delivery
// id) normalises to the same record and does not enqueue duplicate work (AS-147 AC).
func TestNormalizeThenEnqueueIsIdempotent(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newTestDaemon(t, &now)
	payload := `{
		"action": "labeled",
		"repository": {"full_name": "o/r"},
		"sender": {"login": "octocat"},
		"label": {"name": "implementation"},
		"issue": {"number": 42}
	}`
	ev, ok, err := Normalize("issues", "deliv-1", []byte(payload))
	if err != nil || !ok {
		t.Fatalf("normalize: ok=%v err=%v", ok, err)
	}
	first, err := d.EnqueueGitHub(ev)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("want 1 run, got %d", len(first))
	}
	// Same delivery arrives again (GitHub redelivery): no new work.
	ev2, _, _ := Normalize("issues", "deliv-1", []byte(payload))
	second, err := d.EnqueueGitHub(ev2)
	if err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	if len(second) != 1 || second[0].ID != first[0].ID {
		t.Fatalf("redelivery should map to the same run, got %+v", second)
	}
}

// FuzzNormalize checks the parser never panics on arbitrary bytes for every event
// type — a webhook body is adversarial input (testing-strategy: fuzz parsers).
func FuzzNormalize(f *testing.F) {
	f.Add("issues", []byte(`{"action":"labeled","issue":{"number":1},"label":{"name":"x"}}`))
	f.Add("pull_request", []byte(`{"action":"closed","pull_request":{"merged":true}}`))
	f.Add("issue_comment", []byte(`{"action":"created","comment":{"body":"/go"},"issue":{"number":2}}`))
	f.Add("issues", []byte(`not json`))
	f.Fuzz(func(t *testing.T, eventType string, payload []byte) {
		ev, ok, err := Normalize(eventType, "d", payload)
		if err != nil {
			return
		}
		if ok && ev.Kind == "" {
			t.Fatalf("ok event with empty kind: %+v", ev)
		}
	})
}

func assertEvent(t *testing.T, got, want GitHubEvent) {
	t.Helper()
	if got.DeliveryID != want.DeliveryID || got.Kind != want.Kind || got.Repository != want.Repository ||
		got.Label != want.Label || got.Base != want.Base || got.Command != want.Command ||
		got.Number != want.Number || got.Actor != want.Actor || !got.EventTime.Equal(want.EventTime) {
		t.Fatalf("event mismatch:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Labels) != len(want.Labels) {
		t.Fatalf("labels mismatch: got %v want %v", got.Labels, want.Labels)
	}
	for i := range want.Labels {
		if got.Labels[i] != want.Labels[i] {
			t.Fatalf("labels mismatch: got %v want %v", got.Labels, want.Labels)
		}
	}
}

// jsonString renders s as a JSON string literal for embedding in test payloads.
func jsonString(s string) string {
	b := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"', '\\':
			b = append(b, '\\', byte(r))
		case '\n':
			b = append(b, '\\', 'n')
		default:
			b = append(b, string(r)...)
		}
	}
	return string(append(b, '"'))
}
