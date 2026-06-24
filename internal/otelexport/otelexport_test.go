package otelexport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

func intp(n int) *int { return &n }

func ts(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// sampleEvents is a two-turn session: a tool call inside the first turn, then a
// closing usage event per turn.
func sampleEvents() []schema.Block {
	return []schema.Block{
		{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, TS: ts(1000), Text: &schema.TextBody{Text: "hi"}},
		{ID: "t1", Kind: schema.KindToolCall, Role: schema.RoleAssistant, TS: ts(1001), ToolCall: &schema.ToolCallBody{Name: "read"}},
		{ID: "g1", Kind: eventlog.KindUsage, Role: schema.RoleAssistant, TS: ts(1002), StopReason: "tool_use", Provider: &schema.Provider{Model: "model-a"}, Tokens: &schema.Tokens{Input: intp(100), Output: intp(20)}},
		{ID: "g2", Kind: eventlog.KindUsage, Role: schema.RoleAssistant, TS: ts(1005), StopReason: "end_turn", Provider: &schema.Provider{Model: "model-a"}, Tokens: &schema.Tokens{Input: intp(50), Output: intp(10)}},
	}
}

func spansByName(p Payload) map[string][]span {
	out := map[string][]span{}
	for _, rs := range p.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				out[s.Name] = append(out[s.Name], s)
			}
		}
	}
	return out
}

func TestBuildTraceHierarchy(t *testing.T) {
	events := sampleEvents()
	p := BuildTrace("sess_x", events, cost.Summarize(events, cost.Embedded()))
	by := spansByName(p)

	if len(by["session"]) != 1 {
		t.Fatalf("want 1 session span, got %d", len(by["session"]))
	}
	root := by["session"][0]
	if root.ParentSpanID != "" {
		t.Errorf("root session span must have no parent, got %q", root.ParentSpanID)
	}
	if len(by["turn 1"]) != 1 || len(by["turn 2"]) != 1 {
		t.Fatalf("want 1 span each for turn 1/turn 2, got %d/%d", len(by["turn 1"]), len(by["turn 2"]))
	}
	for _, name := range []string{"turn 1", "turn 2"} {
		if by[name][0].ParentSpanID != root.SpanID {
			t.Errorf("%s parent = %q, want root %q", name, by[name][0].ParentSpanID, root.SpanID)
		}
	}
	if len(by["model.call"]) != 2 {
		t.Errorf("want 2 model.call spans, got %d", len(by["model.call"]))
	}
	// The tool span belongs to turn 1 (it preceded the first usage event).
	tools := by["tool.call"]
	if len(tools) != 1 {
		t.Fatalf("want 1 tool.call span, got %d", len(tools))
	}
	if tools[0].ParentSpanID != by["turn 1"][0].SpanID {
		t.Errorf("tool.call parent = %q, want turn 1 %q", tools[0].ParentSpanID, by["turn 1"][0].SpanID)
	}

	// Every span shares one trace ID.
	for _, rs := range p.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				if s.TraceID != root.TraceID {
					t.Errorf("span %q has trace %q, want %q", s.Name, s.TraceID, root.TraceID)
				}
			}
		}
	}
}

func TestBuildTraceCarriesCostAndTokenAttributes(t *testing.T) {
	events := sampleEvents()
	p := BuildTrace("sess_x", events, cost.Summarize(events, cost.Embedded()))
	model := spansByName(p)["model.call"][0]

	attrs := map[string]attributeValue{}
	for _, a := range model.Attributes {
		attrs[a.Key] = a.Value
	}
	if v, ok := attrs["model"]; !ok || v.StringValue == nil || *v.StringValue != "model-a" {
		t.Errorf("model attribute missing/wrong: %+v", attrs["model"])
	}
	if v, ok := attrs["tokens.input"]; !ok || v.IntValue == nil || *v.IntValue != "100" {
		t.Errorf("tokens.input attribute missing/wrong: %+v", attrs["tokens.input"])
	}
	if _, ok := attrs["cost.usd"]; !ok {
		t.Error("cost.usd attribute missing on model.call span")
	}
}

func TestBuildTraceDeterministic(t *testing.T) {
	events := sampleEvents()
	a := BuildTrace("sess_x", events, cost.Summarize(events, cost.Embedded()))
	b := BuildTrace("sess_x", events, cost.Summarize(events, cost.Embedded()))
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Error("BuildTrace is not deterministic for the same session")
	}
}

func TestExportPostsToCollector(t *testing.T) {
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	events := sampleEvents()
	p := BuildTrace("sess_x", events, cost.Summarize(events, cost.Embedded()))
	cfg := Config{Endpoint: srv.URL}
	if err := Export(context.Background(), cfg, p); err != nil {
		t.Fatalf("export: %v", err)
	}
	if gotPath != "/v1/traces" {
		t.Errorf("collector path = %q, want /v1/traces", gotPath)
	}
	if !strings.Contains(string(gotBody), "\"traceId\"") {
		t.Errorf("posted body missing traceId: %s", gotBody)
	}
}

func TestExportDisabledIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	// Empty endpoint => disabled => no POST, even though a server is reachable.
	if err := Export(context.Background(), Config{}, Payload{}); err != nil {
		t.Fatalf("export disabled: %v", err)
	}
	if called {
		t.Error("disabled export must not POST")
	}
}

func TestExportNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := Export(context.Background(), Config{Endpoint: srv.URL}, Payload{}); err == nil {
		t.Error("want error on non-2xx collector response")
	}
}

// fakeConfig is a minimal configReader returning a fixed telemetry section.
type fakeConfig struct {
	section json.RawMessage
	present bool
}

func (f fakeConfig) Decode(path string, v any) (bool, error) {
	if path != "telemetry" || !f.present {
		return false, nil
	}
	return true, json.Unmarshal(f.section, v)
}

func TestConfigFrom(t *testing.T) {
	cfg, warns := ConfigFrom(fakeConfig{present: true, section: json.RawMessage(`{"otel_endpoint":"http://localhost:4318/","otel_timeout_seconds":3}`)})
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if !Enabled(cfg) {
		t.Error("config with endpoint should be enabled")
	}
	if got := cfg.TracesURL(); got != "http://localhost:4318/v1/traces" {
		t.Errorf("TracesURL = %q", got)
	}

	off, _ := ConfigFrom(fakeConfig{present: false})
	if Enabled(off) {
		t.Error("missing telemetry section should be disabled")
	}
}

func TestTracesURLNoDoubleSuffix(t *testing.T) {
	c := Config{Endpoint: "http://host:4318/v1/traces"}
	if got := c.TracesURL(); got != "http://host:4318/v1/traces" {
		t.Errorf("TracesURL double-appended: %q", got)
	}
}
