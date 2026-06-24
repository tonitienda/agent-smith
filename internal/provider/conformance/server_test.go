package conformance

import (
	"net/http"
	"strings"
	"testing"
)

// TestRecordedServerServesAndConsumes is the happy path: a request that matches
// the registered exchange gets the recorded body, and Check reports nothing.
func TestRecordedServerServesAndConsumes(t *testing.T) {
	srv := NewRecordedServer(Exchange{
		Path:         "/v1/messages",
		BodyContains: []string{`"model"`},
		Body:         []byte("hello"),
	})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if diffs := srv.Check(); len(diffs) != 0 {
		t.Errorf("Check = %v, want none", diffs)
	}
}

// TestRecordedServerReportsMismatch proves the server fails loudly with a
// path/body diff when the request does not match the expected exchange — the
// validation FileTransport cannot do.
func TestRecordedServerReportsMismatch(t *testing.T) {
	srv := NewRecordedServer(Exchange{
		Path:         "/v1/messages",
		BodyContains: []string{`"expected"`},
	})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/wrong", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 on mismatch", resp.StatusCode)
	}
	diffs := srv.Check()
	if !containsSubstr(diffs, "path") {
		t.Errorf("expected a path diff, got: %v", diffs)
	}
	if !containsSubstr(diffs, "body missing") {
		t.Errorf("expected a body diff, got: %v", diffs)
	}
}

// TestRecordedServerDetectsUnconsumed proves an exchange that is never requested
// is reported, so a missing adapter call cannot pass silently.
func TestRecordedServerDetectsUnconsumed(t *testing.T) {
	srv := NewRecordedServer(Exchange{Path: "/v1/messages"})
	defer srv.Close()

	diffs := srv.Check()
	if !containsSubstr(diffs, "never requested") {
		t.Errorf("expected an unconsumed-exchange report, got: %v", diffs)
	}
}

// TestRecordedServerReportsUnexpectedRequest proves an extra request beyond the
// registered exchanges is reported rather than silently served.
func TestRecordedServerReportsUnexpectedRequest(t *testing.T) {
	srv := NewRecordedServer() // no exchanges registered
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if !containsSubstr(srv.Check(), "unexpected request") {
		t.Errorf("expected an unexpected-request report, got: %v", srv.Check())
	}
}
