package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
)

// maxErrorBody caps how much of an error response body the adapter reads, so a
// pathological error response cannot exhaust memory.
const maxErrorBody = 64 << 10

// errorEnvelope is the Anthropic error response/event body:
// {"type":"error","error":{"type":...,"message":...}}.
type errorEnvelope struct {
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// readErrorResponse builds a typed *provider.Error from a non-200 Messages
// response: it classifies by HTTP status and the body's error.type, carries the
// provider message, and parses Retry-After for rate-limit/overloaded responses.
// The response body is drained and closed.
func (p *Provider) readErrorResponse(resp *http.Response) *provider.Error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()

	var env errorEnvelope
	_ = json.Unmarshal(body, &env)

	var errType, errMsg string
	if env.Error != nil {
		errType, errMsg = env.Error.Type, env.Error.Message
	}

	kind := classify(resp.StatusCode, errType, errMsg)
	e := provider.New(kind, "%s", errMessage(errType, errMsg, resp.StatusCode))
	e.StatusCode = resp.StatusCode
	if kind == provider.ErrRateLimit || kind == provider.ErrOverloaded {
		e.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	}
	return e
}

// mapStreamError builds a typed *provider.Error from an in-stream error frame,
// which has no HTTP status — classification is by error.type alone.
func mapStreamError(errObj *struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}) *provider.Error {
	var errType, errMsg string
	if errObj != nil {
		errType, errMsg = errObj.Type, errObj.Message
	}
	kind := classify(0, errType, errMsg)
	return provider.New(kind, "%s", errMessage(errType, errMsg, 0))
}

// classify maps an HTTP status and Anthropic error type onto the AS-008
// taxonomy. The error type is authoritative when present; the status is the
// fallback. A 400 whose message signals an oversized prompt becomes
// ErrContextTooLong (the loop must shrink context, not retry as-is).
func classify(status int, errType, errMsg string) provider.ErrorKind {
	switch errType {
	case "authentication_error", "permission_error":
		return provider.ErrAuth
	case "rate_limit_error":
		return provider.ErrRateLimit
	case "overloaded_error", "api_error":
		return provider.ErrOverloaded
	case "invalid_request_error", "request_too_large", "not_found_error":
		if isContextTooLong(errMsg) {
			return provider.ErrContextTooLong
		}
		return provider.ErrInvalidRequest
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return provider.ErrAuth
	case http.StatusTooManyRequests:
		return provider.ErrRateLimit
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		if isContextTooLong(errMsg) {
			return provider.ErrContextTooLong
		}
		return provider.ErrInvalidRequest
	case http.StatusInternalServerError, http.StatusServiceUnavailable, 529:
		return provider.ErrOverloaded
	default:
		return provider.ErrUnknown
	}
}

// isContextTooLong reports whether an error message indicates the input exceeded
// the model's context window (Anthropic: "prompt is too long: N tokens > M
// maximum").
func isContextTooLong(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "too long") || strings.Contains(m, "context window")
}

// errMessage composes a human-readable message from the available signals,
// preferring the provider's own message and never emitting an empty string.
func errMessage(errType, errMsg string, status int) string {
	switch {
	case errType != "" && errMsg != "":
		return errType + ": " + errMsg
	case errMsg != "":
		return errMsg
	case errType != "":
		return errType
	case status != 0:
		return "http " + strconv.Itoa(status)
	default:
		return "anthropic error"
	}
}

// parseRetryAfter parses a Retry-After header, supporting both the delta-seconds
// and HTTP-date forms. It returns 0 when the header is absent or unparsable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
