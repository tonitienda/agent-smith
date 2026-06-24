// Package otelexport turns a session's append-only event log into an
// OpenTelemetry trace and ships it to an OTLP/HTTP endpoint (AS-055, PRD §7.23
// OSS half, §8). Per §8 this is *replayability*, not bit-exact determinism: the
// trace is a faithful, reproducible projection of what the log already records —
// session → turn → model-call / tool-call spans with token and cost attributes —
// not a re-execution.
//
// It is stdlib-only on purpose (no OpenTelemetry SDK dependency, AS-095): the
// OTLP/HTTP JSON encoding is small and stable, so BuildTrace emits the JSON
// payload directly and Export POSTs it. Span and trace IDs are derived
// deterministically from the session and block IDs (crypto/sha256), so the same
// log always yields the same trace — replayable and unit-testable offline against
// a local collector, with no clock or randomness in the hot path.
//
// Export is off by default: an empty endpoint (the default) makes Enabled report
// false and callers skip export entirely.
package otelexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// serviceName is the OTel resource service.name every Agent Smith trace carries.
const serviceName = "agent-smith"

// spanKindInternal is OTLP SpanKind INTERNAL; every Agent Smith span is internal
// work (no client/server RPC semantics apply).
const spanKindInternal = 1

// Payload is a complete OTLP/HTTP trace export request body. Its JSON encoding is
// the OTLP-JSON wire format a collector accepts at /v1/traces.
type Payload struct {
	ResourceSpans []resourceSpans `json:"resourceSpans"`
}

type resourceSpans struct {
	Resource   resource     `json:"resource"`
	ScopeSpans []scopeSpans `json:"scopeSpans"`
}

type resource struct {
	Attributes []attribute `json:"attributes"`
}

type scopeSpans struct {
	Scope scope  `json:"scope"`
	Spans []span `json:"spans"`
}

type scope struct {
	Name string `json:"name"`
}

type span struct {
	TraceID           string      `json:"traceId"`
	SpanID            string      `json:"spanId"`
	ParentSpanID      string      `json:"parentSpanId,omitempty"`
	Name              string      `json:"name"`
	Kind              int         `json:"kind"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []attribute `json:"attributes,omitempty"`
}

// attribute is an OTLP key/value. Exactly one value field is set; OTLP-JSON
// encodes integers as strings (intValue) and floats as numbers (doubleValue).
type attribute struct {
	Key   string         `json:"key"`
	Value attributeValue `json:"value"`
}

type attributeValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

func strAttr(k, v string) attribute { return attribute{Key: k, Value: attributeValue{StringValue: &v}} }

func intAttr(k string, v int) attribute {
	s := strconv.Itoa(v)
	return attribute{Key: k, Value: attributeValue{IntValue: &s}}
}

func floatAttr(k string, v float64) attribute {
	return attribute{Key: k, Value: attributeValue{DoubleValue: &v}}
}

// BuildTrace projects the session's event log into an OTLP trace. The hierarchy is
// session (root) → one span per turn → a model.call span plus a tool.call span for
// each tool the turn invoked. A "turn" is the run of blocks ending at a usage
// event (eventlog.KindUsage), which is exactly where the model-call cost is
// recorded, so turn grouping, model attribution, and cost attribution all come
// from the same boundary. Tool calls appended after the last usage event (a turn
// still in flight) are attached to a final, unpriced turn so nothing is dropped.
func BuildTrace(sessionID string, events []schema.Block, sum cost.Summary) Payload {
	traceID := traceIDFor(sessionID)
	rootID := spanIDFor(sessionID, "session")

	start, end := timeRange(events)
	rootAttrs := []attribute{
		strAttr("session.id", sessionID),
		intAttr("session.turns", len(sum.Turns)),
		intAttr("session.events", len(events)),
		floatAttr("session.cost_usd", sum.TotalUSD),
	}
	spans := []span{{
		TraceID:           traceID,
		SpanID:            rootID,
		Name:              "session",
		Kind:              spanKindInternal,
		StartTimeUnixNano: start,
		EndTimeUnixNano:   end,
		Attributes:        rootAttrs,
	}}

	// turnCost indexes the priced summary by turn order so each turn span carries
	// its model and dollar figure without re-pricing.
	turnIdx := 0
	turnStartTS := start
	var pendingTools []schema.Block
	emitTurn := func(usage *schema.Block, ts string) {
		turnIdx++
		var tc cost.TurnCost
		if turnIdx-1 < len(sum.Turns) {
			tc = sum.Turns[turnIdx-1]
		}
		turnSpanID := spanIDFor(sessionID, fmt.Sprintf("turn:%d", turnIdx))
		spans = append(spans, span{
			TraceID:           traceID,
			SpanID:            turnSpanID,
			ParentSpanID:      rootID,
			Name:              fmt.Sprintf("turn %d", turnIdx),
			Kind:              spanKindInternal,
			StartTimeUnixNano: turnStartTS,
			EndTimeUnixNano:   ts,
			Attributes:        []attribute{intAttr("turn.index", turnIdx)},
		})
		if usage != nil {
			spans = append(spans, span{
				TraceID:           traceID,
				SpanID:            spanIDFor(sessionID, fmt.Sprintf("model:%d", turnIdx)),
				ParentSpanID:      turnSpanID,
				Name:              "model.call",
				Kind:              spanKindInternal,
				StartTimeUnixNano: turnStartTS,
				EndTimeUnixNano:   ts,
				Attributes:        modelAttrs(tc),
			})
		}
		for _, t := range pendingTools {
			spans = append(spans, span{
				TraceID:           traceID,
				SpanID:            spanIDFor(sessionID, "tool:"+t.ID),
				ParentSpanID:      turnSpanID,
				Name:              "tool.call",
				Kind:              spanKindInternal,
				StartTimeUnixNano: unixNano(t),
				EndTimeUnixNano:   unixNano(t),
				Attributes:        []attribute{strAttr("tool.name", t.ToolCall.Name)},
			})
		}
		pendingTools = nil
		turnStartTS = ts
	}

	for i := range events {
		b := events[i]
		switch {
		case b.Kind == eventlog.KindUsage:
			u := b
			emitTurn(&u, unixNano(b))
		case b.Kind == schema.KindToolCall && b.ToolCall != nil:
			pendingTools = append(pendingTools, b)
		}
	}
	// A turn still in flight (tool calls after the final usage event) gets a
	// trailing, model-less turn span so its tool spans are not lost.
	if len(pendingTools) > 0 {
		emitTurn(nil, end)
	}

	return Payload{ResourceSpans: []resourceSpans{{
		Resource:   resource{Attributes: []attribute{strAttr("service.name", serviceName)}},
		ScopeSpans: []scopeSpans{{Scope: scope{Name: serviceName}, Spans: spans}},
	}}}
}

// modelAttrs builds the token + cost attributes for a model.call span from a
// priced turn. Unpriced turns still carry exact token counts; cost reads zero.
func modelAttrs(tc cost.TurnCost) []attribute {
	attrs := []attribute{}
	if tc.Model != "" {
		attrs = append(attrs, strAttr("model", tc.Model))
	}
	attrs = append(attrs,
		intAttr("tokens.input", tc.Tokens.Input),
		intAttr("tokens.output", tc.Tokens.Output),
		intAttr("tokens.cache_read", tc.Tokens.CacheRead),
		intAttr("tokens.cache_write", tc.Tokens.CacheWrite),
		intAttr("tokens.total", tc.Tokens.Total()),
		floatAttr("cost.usd", tc.TotalUSD),
	)
	if tc.StopReason != "" {
		attrs = append(attrs, strAttr("stop_reason", tc.StopReason))
	}
	return attrs
}

// timeRange returns the first and last block timestamps as OTLP nano strings,
// spanning the whole session. An empty log yields "0" for both.
func timeRange(events []schema.Block) (start, end string) {
	if len(events) == 0 {
		return "0", "0"
	}
	return unixNano(events[0]), unixNano(events[len(events)-1])
}

func unixNano(b schema.Block) string {
	return strconv.FormatInt(b.TS.UnixNano(), 10)
}

// traceIDFor derives the 16-byte (32 hex) trace ID deterministically from the
// session ID, so re-exporting the same session reuses the same trace.
func traceIDFor(sessionID string) string {
	sum := sha256.Sum256([]byte("agent-smith:trace:" + sessionID))
	return hex.EncodeToString(sum[:16])
}

// spanIDFor derives an 8-byte (16 hex) span ID from the session ID and a span
// key (e.g. "session", "turn:1", "tool:<block-id>").
func spanIDFor(sessionID, key string) string {
	sum := sha256.Sum256([]byte("agent-smith:span:" + sessionID + ":" + key))
	return hex.EncodeToString(sum[:8])
}

// Export POSTs the OTLP/JSON payload to the configured traces endpoint. It is a
// best-effort side channel: a non-2xx response or transport error is returned so
// the caller can warn, but callers never fail a run on it. A disabled config
// (empty endpoint) is a no-op returning nil.
func Export(ctx context.Context, c Config, p Payload) error {
	if !Enabled(c) {
		return nil
	}
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("otelexport: marshal trace: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TracesURL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("otelexport: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: c.timeout()}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("otelexport: post traces: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("otelexport: collector returned %s", resp.Status)
	}
	return nil
}
