package conformance

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// RecordedServer is a fake vendor API bound to loopback on an ephemeral port
// (AS-133). Unlike FileTransport — which answers any request blindly so it can
// exercise stream/error normalization — a RecordedServer *validates* each
// incoming request against an ordered list of expected Exchanges and fails
// loudly with a method/path/body diff on a mismatch, an unexpected extra
// request, or an exchange that was never consumed. It drives the real provider
// adapters through their normal HTTP client path (TCP, headers, and all), which
// FileTransport cannot, so it guards request serialization in addition to
// response handling.
//
// Construct one with NewRecordedServer, point an adapter at it with the vendor's
// WithBaseURL(srv.URL) plus srv.Client(), run the turn, then call
// AssertConsumed(t) (e.g. via defer) to surface any validation failure.
type RecordedServer struct {
	*httptest.Server

	mu        sync.Mutex
	exchanges []Exchange
	next      int
	problems  []string
}

// Exchange is one expected request/response the server serves, in order. The
// request fields are assertions — a zero field is not checked — and the response
// fields are the raw bytes the fake vendor returns.
type Exchange struct {
	// Method is the expected HTTP method (defaults to POST when empty).
	Method string
	// Path is the expected request path (e.g. "/v1/messages"); empty skips the check.
	Path string
	// BodyContains are substrings that must all appear in the request body. Use
	// them to assert the adapter serialized the model/tools/prompt without pinning
	// the whole body (which carries vendor-specific framing).
	BodyContains []string
	// Status is the response status code (defaults to 200 when zero).
	Status int
	// Header are response headers to set (e.g. Content-Type: text/event-stream).
	Header map[string]string
	// Body is the response body — an SSE stream, a JSON error envelope, etc.
	Body []byte
}

// NewRecordedServer starts a loopback server that serves exchanges in order.
// Close it (defer srv.Close()) when done.
func NewRecordedServer(exchanges ...Exchange) *RecordedServer {
	rs := &RecordedServer{exchanges: exchanges}
	rs.Server = httptest.NewServer(http.HandlerFunc(rs.handle))
	return rs
}

func (rs *RecordedServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.next >= len(rs.exchanges) {
		msg := fmt.Sprintf("unexpected request #%d: %s %s (only %d exchange(s) registered)",
			rs.next+1, r.Method, r.URL.Path, len(rs.exchanges))
		rs.problems = append(rs.problems, msg)
		rs.next++
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	ex := rs.exchanges[rs.next]
	rs.next++

	if diffs := matchRequest(ex, r, body); len(diffs) > 0 {
		msg := fmt.Sprintf("request #%d mismatch:\n  %s", rs.next, strings.Join(diffs, "\n  "))
		rs.problems = append(rs.problems, msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	for k, v := range ex.Header {
		w.Header().Set(k, v)
	}
	status := ex.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(ex.Body)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// matchRequest reports every way r/body diverges from the expected exchange.
func matchRequest(ex Exchange, r *http.Request, body []byte) []string {
	var diffs []string
	if r.Method != methodOr(ex.Method) {
		diffs = append(diffs, fmt.Sprintf("method = %q, want %q", r.Method, methodOr(ex.Method)))
	}
	if ex.Path != "" && r.URL.Path != ex.Path {
		diffs = append(diffs, fmt.Sprintf("path = %q, want %q", r.URL.Path, ex.Path))
	}
	for _, sub := range ex.BodyContains {
		if !bytes.Contains(body, []byte(sub)) {
			diffs = append(diffs, fmt.Sprintf("body missing %q", sub))
		}
	}
	return diffs
}

func methodOr(m string) string {
	if m == "" {
		return http.MethodPost
	}
	return strings.ToUpper(m)
}

// Check returns every validation failure: request mismatches and unexpected
// requests seen so far, plus any registered exchange that was never requested.
// Empty means every request matched and every exchange was consumed.
func (rs *RecordedServer) Check() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := append([]string(nil), rs.problems...)
	for i := rs.next; i < len(rs.exchanges); i++ {
		ex := rs.exchanges[i]
		out = append(out, fmt.Sprintf("exchange #%d (%s %s) was never requested", i+1, methodOr(ex.Method), ex.Path))
	}
	return out
}

// AssertConsumed fails t with a clear diff for every validation failure (call it
// after the adapter run, e.g. via defer).
func (rs *RecordedServer) AssertConsumed(t *testing.T) {
	t.Helper()
	for _, p := range rs.Check() {
		t.Errorf("recorded server: %s", p)
	}
}

// FixtureExchange builds an Exchange whose response is the recorded HTTP fixture
// at path (the same "<case>.http" format FileTransport replays) and whose
// request assertions are reqPath plus bodyContains. It lets a vendor reuse its
// conformance fixtures to drive the request-validating server.
func FixtureExchange(t *testing.T, path, reqPath string, bodyContains ...string) Exchange {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading fixture body %s: %v", path, err)
	}
	hdr := map[string]string{}
	for _, h := range recordedHeaders {
		if v := resp.Header.Get(h); v != "" {
			hdr[h] = v
		}
	}
	return Exchange{
		Path:         reqPath,
		BodyContains: bodyContains,
		Status:       resp.StatusCode,
		Header:       hdr,
		Body:         body,
	}
}
