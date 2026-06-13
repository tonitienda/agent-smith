// Package openai implements the provider.Provider interface (AS-008) against the
// OpenAI API surface (AS-010, PRD §7.1, D6). Like the anthropic adapter it is a
// pure normalizer: it maps the projected, model-facing context (schema.Blocks,
// AS-006) into a vendor wire shape, opens a streaming (SSE) turn, and normalizes
// every stream event into the provider package's uniform Event stream so the
// agent core (loop AS-018, TUI AS-021, accounting AS-020) never sees a
// vendor-specific wire format.
//
// Two wire surfaces, one adapter (AS-002 §4 decision):
//
//   - SurfaceResponses (default) — the OpenAI Responses API. Its typed output[]
//     item model is the closest external analogue to our block log, and it is
//     where reasoning and server tools are first-class. This is the schema-input
//     source of truth for OpenAI.
//   - SurfaceChatCompletions — the legacy /v1/chat/completions surface. This is
//     the de-facto "OpenAI-compatible" wire format spoken by xAI/Grok,
//     OpenRouter, and local servers (Ollama, llama.cpp, vLLM) — the cheap/private
//     tier (§10 Q1). Point base_url at any of them with WithSurface to drive a
//     basic chat turn through one code path. Compatible-endpoint extensions
//     (e.g. Grok reasoning_content) are preserved as optional metadata; missing
//     optional fields (usage, reasoning) degrade gracefully rather than crash.
//
// The single base_url + per-request model knobs are what cover every
// OpenAI-compatible endpoint without extra code. Model selection is per request
// (provider.Request.Model); the adapter holds no global model state.
//
// Auth is via an API key. The key is sourced through the key-storage layer
// (AS-017) once it exists; until then New falls back to the OPENAI_API_KEY
// environment variable when no key is passed explicitly.
//
// Retry/backoff is intentionally not implemented here: failures are classified
// into the AS-008 taxonomy (*provider.Error with Retryable and RetryAfter set),
// and the agentic loop (AS-018) drives one retry policy across every provider
// from those signals.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/tonitienda/agent-smith/internal/provider"
)

// Surface selects which OpenAI wire surface the adapter speaks.
type Surface string

const (
	// SurfaceResponses is the OpenAI Responses API (default), the schema-input
	// source of truth for OpenAI (AS-002 §4).
	SurfaceResponses Surface = "responses"
	// SurfaceChatCompletions is the legacy Chat Completions API — the
	// "OpenAI-compatible" wire format for xAI/Grok, OpenRouter, and local
	// servers. Select it with WithSurface to point base_url at one of those.
	SurfaceChatCompletions Surface = "chat_completions"
)

const (
	// DefaultBaseURL is the OpenAI API origin. Override with WithBaseURL for an
	// OpenAI-compatible endpoint (xAI/Grok, OpenRouter, Ollama) or a test server.
	DefaultBaseURL = "https://api.openai.com"
	// responsesPath is the Responses API endpoint, appended to the base URL.
	responsesPath = "/v1/responses"
	// chatCompletionsPath is the Chat Completions endpoint, appended to the base URL.
	chatCompletionsPath = "/v1/chat/completions"
	// vendorName is this provider's identity; it matches schema.Provider.Vendor on
	// blocks assembled from its stream.
	vendorName = "openai"
)

// Provider is an OpenAI API adapter. It is safe for concurrent use: it holds
// only immutable configuration and an *http.Client (itself safe for concurrent
// use), so the loop may run turns for several sessions against one value.
// Construct it with New.
type Provider struct {
	apiKey     string
	baseURL    string
	surface    Surface
	httpClient *http.Client
}

// compile-time check that *Provider satisfies the interface the core depends on.
var _ provider.Provider = (*Provider)(nil)

// Option configures a Provider built by New.
type Option func(*Provider)

// WithBaseURL overrides the API origin (DefaultBaseURL). A trailing slash is
// tolerated. Use it to point at an OpenAI-compatible endpoint or an httptest
// server.
func WithBaseURL(baseURL string) Option {
	return func(p *Provider) {
		if baseURL != "" {
			p.baseURL = trimTrailingSlash(baseURL)
		}
	}
}

// WithSurface selects the wire surface (SurfaceResponses or
// SurfaceChatCompletions). The default is SurfaceResponses; point an
// OpenAI-compatible endpoint at SurfaceChatCompletions.
func WithSurface(s Surface) Option {
	return func(p *Provider) {
		if s != "" {
			p.surface = s
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

// New builds a Provider. When apiKey is empty it falls back to the
// OPENAI_API_KEY environment variable (the AS-017 key-storage layer will replace
// this fallback). A still-empty key is not an error here — Stream surfaces it as
// a typed ErrAuth so construction stays infallible.
func New(apiKey string, opts ...Option) *Provider {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	p := &Provider{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		surface:    SurfaceResponses,
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name reports the provider identity ("openai").
func (p *Provider) Name() string { return vendorName }

// Stream issues one model turn and returns a normalized Stream of Events. It
// builds the request for the configured surface, opens the SSE response, and
// hands the body to a surface-specific stream that translates each event on
// demand (no whole-response buffering, so deltas render incrementally). Failures
// before the stream starts — a missing key, a malformed request, a non-200
// status — are returned as a typed *provider.Error; mid-stream failures surface
// through Stream.Err.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if p.apiKey == "" {
		return nil, provider.New(provider.ErrAuth, "no API key: set OPENAI_API_KEY or pass a key to openai.New")
	}
	if req.Model == "" {
		return nil, provider.New(provider.ErrInvalidRequest, "request has no model")
	}

	var (
		wire any
		path string
	)
	switch p.surface {
	case SurfaceChatCompletions:
		w, err := buildChatRequest(req)
		if err != nil {
			return nil, provider.New(provider.ErrInvalidRequest, "building request: %v", err)
		}
		wire, path = w, chatCompletionsPath
	default: // SurfaceResponses
		w, err := buildResponsesRequest(req)
		if err != nil {
			return nil, provider.New(provider.ErrInvalidRequest, "building request: %v", err)
		}
		wire, path = w, responsesPath
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, provider.New(provider.ErrInvalidRequest, "marshaling request: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, provider.New(provider.ErrInvalidRequest, "creating http request: %v", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		// Surface context cancellation faithfully so the loop can distinguish it
		// from a provider failure; otherwise treat transport errors as a retryable
		// unknown (a transient network blip the loop may re-attempt).
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		e := provider.New(provider.ErrUnknown, "transport error: %v", err)
		e.Retryable = true
		e.Err = err
		return nil, e
	}

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp)
	}

	if p.surface == SurfaceChatCompletions {
		return newChatStream(resp.Body), nil
	}
	return newResponsesStream(resp.Body), nil
}

// trimTrailingSlash drops a single trailing slash so base_url + path does not
// produce a double slash.
func trimTrailingSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}
