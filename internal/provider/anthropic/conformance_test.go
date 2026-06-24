package anthropic

import (
	"net/http"
	"os"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/conformance"
)

// conformanceModel is substituted into each conformance request. Replay only
// needs a non-empty model (the fixture answers regardless); recording uses it as
// the live model, overridable with SMITH_LIVE_ANTHROPIC_MODEL.
func conformanceModel() string {
	if m := os.Getenv("SMITH_LIVE_ANTHROPIC_MODEL"); m != "" {
		return m
	}
	return "claude-sonnet-4-6"
}

// TestConformance runs the shared provider conformance suite (AS-012) against
// the Anthropic adapter from recorded fixtures — no network, no API key. It
// proves this adapter normalizes the Messages wire format to the same Events as
// every other provider.
func TestConformance(t *testing.T) {
	conformance.Run(t, conformanceModel(), func(t *testing.T, c conformance.Case) provider.Provider {
		path := conformance.FixturePath(conformance.FixtureDir, c.Name)
		return New("test-key", WithHTTPClient(&http.Client{Transport: conformance.FileTransport(t, path)}))
	})
}

// TestRecordConformance regenerates the conformance fixtures from live calls. It
// is skipped unless SMITH_RECORD=1 and ANTHROPIC_API_KEY are set, so it never
// runs in CI; `make record-fixtures` drives it. After recording, reconcile any
// wire-format change with the conformance.Want expectations.
func TestRecordConformance(t *testing.T) {
	if os.Getenv("SMITH_RECORD") != "1" {
		t.Skip("set SMITH_RECORD=1 (and ANTHROPIC_API_KEY) to re-record fixtures; see make record-fixtures")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	conformance.Record(t, conformanceModel(), func(t *testing.T, c conformance.Case) provider.Provider {
		path := conformance.FixturePath(conformance.FixtureDir, c.Name)
		rt := conformance.RecordingTransport(http.DefaultTransport, path)
		return New("", WithHTTPClient(&http.Client{Transport: rt}))
	})
}

// TestFixtureMetadata asserts every conformance fixture is classified as a
// synthetic edge case or a redacted real capture (AS-133), so the corpus stays
// auditable as real captures land.
func TestFixtureMetadata(t *testing.T) {
	conformance.AssertFixtureMetadata(t, conformance.FixtureDir)
}
