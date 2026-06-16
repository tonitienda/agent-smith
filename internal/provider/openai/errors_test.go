package openai

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
		errCode  string
		errMsg   string
		wantKind provider.ErrorKind
		retry    bool
	}{
		{"auth", 401, "invalid_request_error", "invalid_api_key", "bad key", provider.ErrAuth, false},
		{"permission", 403, "permission_error", "", "no access", provider.ErrAuth, false},
		{"rate limit", 429, "rate_limit_error", "rate_limit_exceeded", "slow down", provider.ErrRateLimit, true},
		{"quota", 429, "insufficient_quota", "insufficient_quota", "no credit", provider.ErrInvalidRequest, false},
		{"overloaded", 503, "server_error", "", "unavailable", provider.ErrOverloaded, true},
		{"server", 500, "server_error", "", "boom", provider.ErrOverloaded, true},
		{"invalid", 400, "invalid_request_error", "", "bad field", provider.ErrInvalidRequest, false},
		{"context too long", 400, "invalid_request_error", "context_length_exceeded", "maximum context length is 128000 tokens", provider.ErrContextTooLong, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"error":{"type":"` + tc.errType + `","code":"` + tc.errCode + `","message":"` + tc.errMsg + `"}}`
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
	body := `{"error":{"type":"rate_limit_error","message":"slow"}}`
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

func TestParseRetryAfterClockSkew(t *testing.T) {
	serverNow := time.Now().Add(-10 * time.Minute)
	date := serverNow.UTC().Format(http.TimeFormat)
	retryAt := serverNow.Add(40 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(retryAt, date); got != 40*time.Second {
		t.Errorf("parseRetryAfter with Date = %v, want 40s (server-relative)", got)
	}
}

// TestChatStreamErrorChunk checks a mid-stream Chat Completions error chunk
// terminates the stream with the mapped taxonomy, after delivering the events
// that preceded it.
func TestChatStreamErrorChunk(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		``,
		`data: {"error":{"type":"server_error","message":"overloaded"}}`,
		``,
	}, "\n")
	p := sseServer(t, SurfaceChatCompletions, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events, err := provider.Collect(s)
	if provider.KindOf(err) != provider.ErrOverloaded {
		t.Errorf("kind = %v, want overloaded", provider.KindOf(err))
	}
	if !provider.IsRetryable(err) {
		t.Error("overloaded stream error should be retryable")
	}
	if len(events) == 0 {
		t.Error("expected events streamed before the error to be delivered")
	}
}

// TestResponsesStreamFailedEvent checks a response.failed event terminates the
// Responses stream with the mapped taxonomy.
func TestResponsesStreamFailedEvent(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r","model":"gpt-5"}}`,
		``,
		`data: {"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"rate_limit_exceeded","message":"too fast"}}}`,
		``,
	}, "\n")
	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); provider.KindOf(err) != provider.ErrRateLimit {
		t.Errorf("kind = %v, want rate_limit", provider.KindOf(err))
	}
}

func TestResponsesStreamNestedErrorEvent(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r","model":"gpt-5"}}`,
		``,
		`data: {"type":"error","error":{"type":"invalid_request_error","code":"model_not_found","message":"model ` + "gpt-5.4-mini" + ` not found"}}`,
		``,
	}, "\n")
	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); provider.KindOf(err) != provider.ErrInvalidRequest {
		t.Fatalf("kind = %v, want invalid_request", provider.KindOf(err))
	} else if !strings.Contains(err.Error(), "model gpt-5.4-mini not found") {
		t.Fatalf("error = %v, want nested provider message", err)
	}
}

func TestResponsesStreamMalformedFrame(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"r"}}`,
		``,
		`data: {not valid json`,
		``,
	}, "\n")
	p := sseServer(t, SurfaceResponses, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); err == nil {
		t.Fatal("Collect err = nil, want decode error")
	}
}

func TestStreamContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	p := New("k", WithBaseURL(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Stream(ctx, provider.Request{Model: "m"}); err == nil {
		t.Fatal("Stream err = nil, want context.Canceled")
	}
}
