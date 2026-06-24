package openai

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/conformance"
	"github.com/tonitienda/agent-smith/schema"
)

// TestRecordedServerLargeToolArgs drives the real Responses adapter through its
// normal HTTP client path against a request-validating loopback server (AS-133):
// the request must reach /v1/responses carrying the model, and a 1 KiB tool
// argument must survive the streaming round trip byte-for-byte.
func TestRecordedServerLargeToolArgs(t *testing.T) {
	path := conformance.FixturePath(conformance.FixtureDir, "large_tool_args")
	srv := conformance.NewRecordedServer(
		conformance.FixtureExchange(t, path, responsesPath, `"model"`, `"read_file"`),
	)
	defer srv.Close()

	p := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	s, err := p.Stream(t.Context(), provider.Request{
		Model: "gpt-5",
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
	srv.AssertConsumed(t)

	assertLargeToolArgs(t, got)
}

// TestRecordedServerLargeToolArgsChat covers the third supported vendor shape:
// the OpenAI-compatible chat/completions projection (xAI/Grok-style tool_calls
// deltas), driven through a request-validating loopback server.
func TestRecordedServerLargeToolArgsChat(t *testing.T) {
	path := conformance.FixturePath(conformance.FixtureDir, "large_tool_args_chat")
	srv := conformance.NewRecordedServer(
		conformance.FixtureExchange(t, path, chatCompletionsPath, `"model"`, `"read_file"`),
	)
	defer srv.Close()

	p := New("test-key", WithSurface(SurfaceChatCompletions), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	s, err := p.Stream(t.Context(), provider.Request{
		Model: "grok-4.3",
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
	srv.AssertConsumed(t)
	assertLargeToolArgs(t, got)
}

func assertLargeToolArgs(t *testing.T, got conformance.Result) {
	t.Helper()
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
