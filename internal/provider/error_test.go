package provider_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
)

func TestErrorDefaultRetryable(t *testing.T) {
	cases := []struct {
		kind          provider.ErrorKind
		wantRetryable bool
	}{
		{provider.ErrAuth, false},
		{provider.ErrRateLimit, true},
		{provider.ErrOverloaded, true},
		{provider.ErrContextTooLong, false},
		{provider.ErrInvalidRequest, false},
		{provider.ErrUnknown, false},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			err := provider.New(c.kind, "boom")
			if err.Retryable != c.wantRetryable {
				t.Errorf("Retryable = %v, want %v", err.Retryable, c.wantRetryable)
			}
			if provider.IsRetryable(err) != c.wantRetryable {
				t.Errorf("IsRetryable = %v, want %v", provider.IsRetryable(err), c.wantRetryable)
			}
			if provider.KindOf(err) != c.kind {
				t.Errorf("KindOf = %v, want %v", provider.KindOf(err), c.kind)
			}
		})
	}
}

func TestAsErrorUnwrapsChain(t *testing.T) {
	cause := errors.New("connection reset")
	pe := provider.New(provider.ErrRateLimit, "slow down")
	pe.RetryAfter = 2 * time.Second
	pe.StatusCode = 429
	pe.Err = cause
	wrapped := fmt.Errorf("turn failed: %w", pe)

	got, ok := provider.AsError(wrapped)
	if !ok {
		t.Fatal("AsError did not find a *Error in the chain")
	}
	if got.Kind != provider.ErrRateLimit || got.RetryAfter != 2*time.Second || got.StatusCode != 429 {
		t.Errorf("got %+v, want rate_limit/2s/429", got)
	}
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is did not reach the wrapped cause through Unwrap")
	}
}

func TestKindAndRetryableOnNonProviderError(t *testing.T) {
	plain := errors.New("nope")
	if provider.KindOf(plain) != provider.ErrUnknown {
		t.Errorf("KindOf(plain) = %v, want unknown", provider.KindOf(plain))
	}
	if provider.IsRetryable(plain) || provider.IsRetryable(nil) {
		t.Error("non-provider / nil errors must not be retryable")
	}
}

func TestErrorMessageVariants(t *testing.T) {
	cases := []struct {
		name string
		err  *provider.Error
		want string
	}{
		{"kind only", &provider.Error{Kind: provider.ErrAuth}, "provider: auth"},
		{"message", provider.New(provider.ErrAuth, "bad key"), "provider: auth: bad key"},
		{"cause", &provider.Error{Kind: provider.ErrUnknown, Err: errors.New("eof")}, "provider: unknown: eof"},
		{"message+cause", &provider.Error{Kind: provider.ErrOverloaded, Message: "busy", Err: errors.New("503")}, "provider: overloaded: busy: 503"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
		})
	}
}
