package provider

import (
	"errors"
	"fmt"
	"time"
)

// ErrorKind is the provider-agnostic error taxonomy (AS-008). Adapters classify
// every vendor failure into one of these so the loop drives a single
// retry/backoff policy without knowing which provider produced the error.
type ErrorKind string

const (
	// ErrAuth: bad or missing credentials, or a permission failure (HTTP 401/403).
	// Not retryable — the loop must surface it, not spin.
	ErrAuth ErrorKind = "auth"
	// ErrRateLimit: the request was rate limited (HTTP 429). Retryable after a
	// backoff, honoring RetryAfter when the provider supplies it.
	ErrRateLimit ErrorKind = "rate_limit"
	// ErrOverloaded: the provider is temporarily overloaded or unavailable
	// (Anthropic overloaded_error, HTTP 503/529). Retryable after a backoff.
	ErrOverloaded ErrorKind = "overloaded"
	// ErrContextTooLong: the input exceeded the model's context window. Not
	// retryable as-is — the loop must shrink context (compaction, /clean) first.
	ErrContextTooLong ErrorKind = "context_too_long"
	// ErrInvalidRequest: a malformed or rejected request (HTTP 400/422). Not
	// retryable — retrying the identical request fails identically.
	ErrInvalidRequest ErrorKind = "invalid_request"
	// ErrUnknown: a failure the adapter could not classify (e.g. an unexpected
	// status or a transport error). Conservatively not retryable by default;
	// adapters may still set Retryable for transient transport errors.
	ErrUnknown ErrorKind = "unknown"
)

// defaultRetryable reports the conventional retry posture for a kind. Adapters
// may override it on the Error (e.g. a transient transport ErrUnknown), but this
// gives the loop a sane default without inspecting the kind everywhere.
func (k ErrorKind) defaultRetryable() bool {
	switch k {
	case ErrRateLimit, ErrOverloaded:
		return true
	default:
		return false
	}
}

// Error is a normalized provider failure carrying the taxonomy and the signals
// the loop needs to react: whether to retry, how long to wait, and the
// underlying cause for unwrapping. Both Provider.Stream's immediate error and a
// Stream's terminating Err use this type when the failure is provider-shaped.
type Error struct {
	// Kind is the taxonomy bucket.
	Kind ErrorKind
	// Message is a human-readable description (the provider's message when
	// available), used by Error.
	Message string
	// Retryable reports whether the loop may retry after a backoff. Defaults to
	// the kind's conventional posture (see New) but adapters may override it.
	Retryable bool
	// RetryAfter is the provider-suggested minimum wait before retrying (from a
	// Retry-After header or equivalent); zero when unspecified.
	RetryAfter time.Duration
	// StatusCode is the HTTP status that produced the error, or 0 when not
	// HTTP-derived.
	StatusCode int
	// Err is the underlying transport/decode error, exposed via Unwrap so callers
	// can errors.Is/As the cause.
	Err error
}

// New builds an *Error of kind with a message, defaulting Retryable to the
// kind's conventional posture. Adapters layer the HTTP status, RetryAfter, or a
// wrapped cause on the returned value as needed.
func New(kind ErrorKind, format string, args ...any) *Error {
	return &Error{
		Kind:      kind,
		Message:   fmt.Sprintf(format, args...),
		Retryable: kind.defaultRetryable(),
	}
}

// Error implements error. It includes the kind so logs are self-describing and
// the wrapped cause when present.
func (e *Error) Error() string {
	switch {
	case e == nil:
		return "<nil provider.Error>"
	case e.Message != "" && e.Err != nil:
		return fmt.Sprintf("provider: %s: %s: %v", e.Kind, e.Message, e.Err)
	case e.Message != "":
		return fmt.Sprintf("provider: %s: %s", e.Kind, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("provider: %s: %v", e.Kind, e.Err)
	default:
		return fmt.Sprintf("provider: %s", e.Kind)
	}
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Err }

// AsError extracts a *Error from err's chain, reporting whether one was found.
// The loop uses it to read the taxonomy off any returned error.
func AsError(err error) (*Error, bool) {
	var pe *Error
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}

// KindOf returns the taxonomy kind of err, or ErrUnknown if err is not (and does
// not wrap) a *Error.
func KindOf(err error) ErrorKind {
	if pe, ok := AsError(err); ok {
		return pe.Kind
	}
	return ErrUnknown
}

// IsRetryable reports whether err is a *Error the loop may retry after a backoff.
// A non-provider error (including nil) is treated as not retryable.
func IsRetryable(err error) bool {
	pe, ok := AsError(err)
	return ok && pe.Retryable
}
