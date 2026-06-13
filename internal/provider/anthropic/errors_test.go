package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
)

// errorServer replays a non-200 response with the given status, body, and
// optional Retry-After header.
func errorServer(t *testing.T, status int, body, retryAfter string) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New("k", WithBaseURL(srv.URL))
}

func TestStreamHTTPErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		errType  string
		errMsg   string
		wantKind provider.ErrorKind
		retry    bool
	}{
		{"auth", 401, "authentication_error", "invalid key", provider.ErrAuth, false},
		{"permission", 403, "permission_error", "no access", provider.ErrAuth, false},
		{"rate limit", 429, "rate_limit_error", "slow down", provider.ErrRateLimit, true},
		{"overloaded", 529, "overloaded_error", "overloaded", provider.ErrOverloaded, true},
		{"server", 500, "api_error", "boom", provider.ErrOverloaded, true},
		{"invalid", 400, "invalid_request_error", "bad field", provider.ErrInvalidRequest, false},
		{"context too long", 400, "invalid_request_error", "prompt is too long: 300000 tokens > 200000 maximum", provider.ErrContextTooLong, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"type":"error","error":{"type":"` + tc.errType + `","message":"` + tc.errMsg + `"}}`
			p := errorServer(t, tc.status, body, "")
			_, err := p.Stream(context.Background(), provider.Request{Model: "m"})
			pe, ok := provider.AsError(err)
			if !ok {
				t.Fatalf("err = %v, want *provider.Error", err)
			}
			if pe.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", pe.Kind, tc.wantKind)
			}
			if pe.Retryable != tc.retry {
				t.Errorf("retryable = %v, want %v", pe.Retryable, tc.retry)
			}
			if pe.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", pe.StatusCode, tc.status)
			}
			if !strings.Contains(pe.Message, tc.errMsg) {
				t.Errorf("message = %q, want to contain %q", pe.Message, tc.errMsg)
			}
		})
	}
}

func TestStreamRetryAfterSeconds(t *testing.T) {
	body := `{"type":"error","error":{"type":"rate_limit_error","message":"slow"}}`
	p := errorServer(t, 429, body, "5")
	_, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	pe, _ := provider.AsError(err)
	if pe == nil || pe.RetryAfter != 5*time.Second {
		t.Errorf("retry-after = %v, want 5s", pe)
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	d := parseRetryAfter(future, "")
	if d <= 0 || d > 31*time.Second {
		t.Errorf("parseRetryAfter(date) = %v, want ~30s", d)
	}
	if parseRetryAfter("", "") != 0 || parseRetryAfter("garbage", "") != 0 {
		t.Error("empty/garbage Retry-After should parse to 0")
	}
}

// TestParseRetryAfterClockSkew checks that when a Date header is present the wait
// is measured against the server's clock, so a skewed client clock does not
// distort it: Retry-After is 40s after Date regardless of where "now" is.
func TestParseRetryAfterClockSkew(t *testing.T) {
	serverNow := time.Now().Add(-10 * time.Minute) // pretend the client clock is 10m fast
	date := serverNow.UTC().Format(http.TimeFormat)
	retryAt := serverNow.Add(40 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(retryAt, date); got != 40*time.Second {
		t.Errorf("parseRetryAfter with Date = %v, want 40s (server-relative)", got)
	}
}

func TestStreamMidStreamErrorFrame(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"m","model":"m","usage":{"input_tokens":1}}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
		``,
		`event: error`,
		`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
		``,
	}, "\n")

	p := sseServer(t, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events, err := provider.Collect(s)
	if err == nil {
		t.Fatal("Collect err = nil, want overloaded error")
	}
	if provider.KindOf(err) != provider.ErrOverloaded {
		t.Errorf("kind = %v, want overloaded", provider.KindOf(err))
	}
	if !provider.IsRetryable(err) {
		t.Error("overloaded stream error should be retryable")
	}
	// The events before the error frame are still delivered.
	if len(events) == 0 {
		t.Error("expected the events streamed before the error to be delivered")
	}
}

func TestStreamMalformedDataFrame(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"m","model":"m"}}`,
		``,
		`data: {not valid json`,
		``,
	}, "\n")
	p := sseServer(t, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); err == nil {
		t.Fatal("Collect err = nil, want decode error")
	}
}

func TestStreamContextCanceled(t *testing.T) {
	// A server that blocks forever; canceling the context must abort the request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	p := New("k", WithBaseURL(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Stream(ctx, provider.Request{Model: "m"})
	if err == nil {
		t.Fatal("Stream err = nil, want context.Canceled")
	}
}
