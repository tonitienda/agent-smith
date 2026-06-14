package conformance

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// FixtureDir is the directory, relative to a vendor package, that holds its
// conformance fixtures. Each fixture is a raw HTTP response named "<case>.http".
const FixtureDir = "testdata/conformance"

// FixturePath returns the fixture file path for a case under dir.
func FixturePath(dir, caseName string) string {
	return filepath.Join(dir, caseName+".http")
}

// FileTransport returns an http.RoundTripper that answers every request with the
// recorded HTTP response in the fixture at path, regardless of the request — the
// adapter under test still builds and "sends" its request, but the response is
// the fixture, so the suite runs with zero network access. The file is read
// eagerly so a missing fixture fails the test on the test goroutine.
func FileTransport(t *testing.T, path string) http.RoundTripper {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), req)
		if err != nil {
			return nil, fmt.Errorf("parsing fixture %s: %w", path, err)
		}
		return resp, nil
	})
}

// RecordingTransport returns an http.RoundTripper that performs the request live
// through next (use http.DefaultTransport), writes the full response to the
// fixture at path, and returns a re-readable copy so the adapter can still
// consume it. It powers the `make record-fixtures` refresh flow.
func RecordingTransport(next http.RoundTripper, path string) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading live response body: %w", err)
		}
		if err := writeFixture(path, resp, body); err != nil {
			return nil, fmt.Errorf("writing fixture %s: %w", path, err)
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		return resp, nil
	})
}

// recordedHeaders are the response headers preserved in a fixture. Only the
// fields that affect normalization are kept (content type, and the retry timing
// the error taxonomy reads); everything else (auth echoes, request ids, cookies)
// is dropped so fixtures stay small and carry no secrets.
var recordedHeaders = []string{"Content-Type", "Retry-After", "Date"}

// writeFixture serializes a captured response to a raw HTTP/1.1 fixture: the
// status line, the allowlisted headers plus a computed Content-Length, and the
// body. Content-Length makes replay deterministic for streaming (SSE) bodies
// that the live response delivered chunked.
func writeFixture(path string, resp *http.Response, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // fixture dir
		return err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	for _, h := range recordedHeaders {
		if v := resp.Header.Get(h); v != "" {
			fmt.Fprintf(&buf, "%s: %s\r\n", h, v)
		}
	}
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	return os.WriteFile(path, buf.Bytes(), 0o644) //nolint:gosec // fixtures are not secret
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
