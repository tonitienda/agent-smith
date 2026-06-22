// Package anthropic implements the provider.Provider interface (AS-008) against
// the Anthropic Messages API (AS-009, PRD §7.1, D6). It is a concrete adapter:
// it maps the projected, model-facing context (schema.Blocks, AS-006) into the
// Messages wire shape, opens a streaming (SSE) turn, and normalizes every
// Anthropic stream event into the provider package's uniform Event stream so the
// agent core (loop AS-018, TUI AS-021, accounting AS-020) never sees a
// vendor-specific wire format. The vendor-specific normalization — the product's
// core IP (PRD §9) — lives entirely here and never leaks out of the package.
//
// Auth is via an API key. The composition root resolves it through the key-storage
// layer (AS-017, internal/credential): the ANTHROPIC_API_KEY env var, else the OS
// keychain. When no key is passed explicitly, New still falls back to
// ANTHROPIC_API_KEY directly so the package is usable on its own.
//
// Retry/backoff is intentionally not implemented inside the provider: failures
// are classified into the AS-008 taxonomy (*provider.Error with Retryable and
// RetryAfter set), and the agentic loop (AS-018) drives one retry policy across
// every provider from those signals. This keeps the adapter a pure normalizer.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/tonitienda/agent-smith/internal/provider"
)

const (
	// DefaultBaseURL is the Anthropic API origin. Override with WithBaseURL for a
	// proxy, a gateway, or a test server (httptest).
	DefaultBaseURL = "https://api.anthropic.com"
	// DefaultVersion is the anthropic-version header value the adapter pins. It is
	// a dated contract; bumping it is an explicit, reviewed change.
	DefaultVersion = "2023-06-01"
	// DefaultMaxTokens is the max_tokens used when a request leaves
	// SamplingParams.MaxTokens unset. Anthropic requires max_tokens on every
	// request, so the adapter must supply a value rather than omit it.
	DefaultMaxTokens = 4096
	// messagesPath is the Messages API endpoint, appended to the base URL.
	messagesPath = "/v1/messages"
	// vendorName is this provider's identity; it matches schema.Provider.Vendor on
	// blocks assembled from its stream.
	vendorName = "anthropic"
)

// Provider is an Anthropic Messages API adapter. It is safe for concurrent use:
// it holds only immutable configuration and an *http.Client (itself safe for
// concurrent use), so the loop may run turns for several sessions against one
// value. Construct it with New.
type Provider struct {
	apiKey     string
	baseURL    string
	version    string
	maxTokens  int
	httpClient *http.Client
}

// compile-time check that *Provider satisfies the interface the core depends on.
var _ provider.Provider = (*Provider)(nil)

// Option configures a Provider built by New.
type Option func(*Provider)

// WithBaseURL overrides the API origin (DefaultBaseURL). A trailing slash is
// tolerated. Use it to point at a proxy/gateway or an httptest server.
func WithBaseURL(baseURL string) Option {
	return func(p *Provider) {
		if baseURL != "" {
			p.baseURL = baseURL
		}
	}
}

// WithVersion overrides the anthropic-version header (DefaultVersion).
func WithVersion(version string) Option {
	return func(p *Provider) {
		if version != "" {
			p.version = version
		}
	}
}

// WithMaxTokens overrides the default max_tokens (DefaultMaxTokens) applied when
// a request does not set SamplingParams.MaxTokens.
func WithMaxTokens(n int) Option {
	return func(p *Provider) {
		if n > 0 {
			p.maxTokens = n
		}
	}
}

// WithHTTPClient overrides the HTTP client. The default client has no timeout,
// because a streaming turn is long-lived and cancellation is driven by the
// request context; set a custom client only if you understand that trade-off.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) {
		if c != nil {
			p.httpClient = c
		}
	}
}

// New builds a Provider. The composition root passes a key resolved through the
// AS-017 credential layer; when apiKey is empty New still falls back to the
// ANTHROPIC_API_KEY environment variable so the package works standalone. A
// still-empty key is not an error here — Stream surfaces it as a typed ErrAuth so
// construction stays infallible.
func New(apiKey string, opts ...Option) *Provider {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	p := &Provider{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		version:    DefaultVersion,
		maxTokens:  DefaultMaxTokens,
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name reports the provider identity ("anthropic").
func (p *Provider) Name() string { return vendorName }

// Stream issues one model turn and returns a normalized Stream of Events. It
// builds the Messages request from req, opens the SSE response, and hands the
// body to an sseStream that translates each Anthropic event on demand (no
// whole-response buffering, so deltas render incrementally). Failures before the
// stream starts — a missing key, a malformed request, a non-200 status — are
// returned as a typed *provider.Error; mid-stream failures surface through
// Stream.Err.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if p.apiKey == "" {
		return nil, provider.New(provider.ErrAuth, "no API key: set ANTHROPIC_API_KEY or pass a key to anthropic.New")
	}
	if req.Model == "" {
		return nil, provider.New(provider.ErrInvalidRequest, "request has no model")
	}

	wire, err := buildWireRequest(req, p.maxTokens)
	if err != nil {
		return nil, provider.New(provider.ErrInvalidRequest, "building request: %v", err)
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, provider.New(provider.ErrInvalidRequest, "marshaling request: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+messagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, provider.New(provider.ErrInvalidRequest, "creating http request: %v", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", p.version)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		// Surface context cancellation faithfully so the loop can distinguish it
		// from a provider failure; otherwise treat transport errors as a
		// retryable unknown (a transient network blip the loop may re-attempt).
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		e := provider.New(provider.ErrUnknown, "transport error: %v", err)
		e.Retryable = true
		e.Err = err
		return nil, e
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.readErrorResponse(resp)
	}

	return newSSEStream(resp.Body), nil
}
