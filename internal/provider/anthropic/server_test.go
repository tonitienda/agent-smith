package anthropic

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/conformance"
	"github.com/tonitienda/agent-smith/schema"
)

// TestRecordedServerLargeToolArgs drives the real adapter through its normal HTTP
// client path against a request-validating loopback server (AS-133): the request
// must reach /v1/messages carrying the model, and a 1 KiB tool argument must
// survive the streaming round trip byte-for-byte. This guards request
// serialization (which FileTransport bypasses) in addition to tool-arg
// preservation under a large payload.
func TestRecordedServerLargeToolArgs(t *testing.T) {
	path := conformance.FixturePath(conformance.FixtureDir, "large_tool_args")
	srv := conformance.NewRecordedServer(
		conformance.FixtureExchange(t, path, messagesPath, `"model"`, `"read_file"`),
	)
	defer srv.Close()
	defer srv.AssertConsumed(t)

	p := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	s, err := p.Stream(t.Context(), provider.Request{
		Model: "claude-sonnet-4-6",
		Tools: []provider.ToolDef{{
			Name:        "read_file",
			Description: "Read a file from the workspace.",
			InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
		Context: []schema.Block{{
			ID: "u1", Kind: schema.KindText, Role: schema.RoleUser,
			Text: &schema.TextBody{Text: "Read the long path."},
		}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got, err := conformance.Assemble(s)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	wantArgs := `{"path":"` + strings.Repeat("A", 1024) + `"}`
	if len(got.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(got.Blocks))
	}
	b := got.Blocks[0]
	if b.Kind != schema.KindToolCall || b.ToolName != "read_file" {
		t.Fatalf("block = %q/%q, want tool_call/read_file", b.Kind, b.ToolName)
	}
	if b.ArgumentsRaw != wantArgs {
		t.Errorf("large tool args not preserved verbatim: len(got)=%d, len(want)=%d", len(b.ArgumentsRaw), len(wantArgs))
	}
}
