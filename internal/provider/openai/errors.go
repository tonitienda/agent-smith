package openai

import (
	"encoding/json"
	"fmt"
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

// errorEnvelope is the OpenAI error response/chunk body:
// {"error":{"message":...,"type":...,"code":...}}. Both surfaces and all
// OpenAI-compatible endpoints share this shape.
type errorEnvelope struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// readErrorResponse builds a typed *provider.Error from a non-200 response: it
// classifies by HTTP status and the body's error type/code, carries the provider
// message, and parses Retry-After for rate-limit/overloaded responses. The
// response body is drained and closed.
func readErrorResponse(resp *http.Response) *provider.Error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()

	var env errorEnvelope
	_ = json.Unmarshal(body, &env)

	var errType, errCode, errMsg string
	if env.Error != nil {
		errType, errCode, errMsg = env.Error.Type, env.Error.Code, env.Error.Message
	}

	kind := classify(resp.StatusCode, errType, errCode, errMsg)
	e := provider.New(kind, "%s", errMessage(errType, errMsg, resp.StatusCode))
	e.StatusCode = resp.StatusCode
	if kind == provider.ErrRateLimit || kind == provider.ErrOverloaded {
		e.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), resp.Header.Get("Date"))
	}
	return e
}

// mapChatErrorChunk builds a typed *provider.Error from an in-stream Chat
// Completions error chunk, which has no HTTP status — classification is by
// type/code/message alone.
func mapChatErrorChunk(errType, errCode, errMsg string) *provider.Error {
	kind := classify(0, errType, errCode, errMsg)
	return provider.New(kind, "%s", errMessage(errType, errMsg, 0))
}

// mapResponsesErrorEvent builds a typed *provider.Error from a top-level
// Responses "error" event.
func mapResponsesErrorEvent(f *responsesFrame) *provider.Error {
	kind := classify(0, "", f.Code, f.Message)
	return provider.New(kind, "%s", errMessage("", f.Message, 0))
}

// mapResponsesFailure builds a typed *provider.Error from a "response.failed"
// event, reading the error off the response object.
func mapResponsesFailure(f *responsesFrame) *provider.Error {
	var code, msg string
	if f.Response != nil && f.Response.Error != nil {
		code, msg = f.Response.Error.Code, f.Response.Error.Message
	}
	kind := classify(0, "", code, msg)
	return provider.New(kind, "%s", errMessage("", msg, 0))
}

// classify maps an HTTP status and OpenAI error type/code onto the AS-008
// taxonomy. The code/type/message are authoritative when present; the status is
// the fallback. A context-length signal becomes ErrContextTooLong (the loop must
// shrink context, not retry as-is); insufficient_quota is a non-retryable
// billing failure, not a transient rate limit.
func classify(status int, errType, errCode, errMsg string) provider.ErrorKind {
	switch errCode {
	case "context_length_exceeded", "string_above_max_length":
		return provider.ErrContextTooLong
	case "insufficient_quota":
		return provider.ErrInvalidRequest
	case "rate_limit_exceeded":
		return provider.ErrRateLimit
	case "invalid_api_key":
		// OpenAI reports a bad key as type invalid_request_error + this code; the
		// code is authoritative so it is not misfiled as a generic 400.
		return provider.ErrAuth
	}
	switch errType {
	case "authentication_error", "permission_error":
		return provider.ErrAuth
	case "insufficient_quota":
		return provider.ErrInvalidRequest
	case "rate_limit_error":
		return provider.ErrRateLimit
	case "server_error":
		return provider.ErrOverloaded
	case "invalid_request_error":
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
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return provider.ErrOverloaded
	default:
		return provider.ErrUnknown
	}
}

// isContextTooLong reports whether an error message indicates the input exceeded
// the model's context window (OpenAI: "maximum context length is N tokens…").
func isContextTooLong(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "maximum context length") ||
		strings.Contains(m, "context length") ||
		strings.Contains(m, "context window") ||
		strings.Contains(m, "too long")
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
		return "openai error"
	}
}

// parseRetryAfter parses a Retry-After header, supporting both the delta-seconds
// and HTTP-date forms. For the date form it measures the wait against the
// response's Date header (the server's clock) when available, so the result is
// immune to client-server clock skew, falling back to the local clock otherwise.
// It returns 0 when the header is absent or unparsable.
func parseRetryAfter(v, date string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if date != "" {
			if serverTime, err := http.ParseTime(date); err == nil {
				if d := t.Sub(serverTime); d > 0 {
					return d
				}
				return 0
			}
		}
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// errInvalidToolArgs is the error returned when a tool call carries arguments
// that are not valid JSON.
func errInvalidToolArgs(toolUseID string) error {
	return fmt.Errorf("tool_call %q has invalid JSON arguments", toolUseID)
}
