package insightsmodel

import (
	"context"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/insights"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/schema"
)

func iptr(n int) *int { return &n }

func textDelta(s string) provider.Event {
	return provider.Event{Type: provider.EventTextDelta, TextDelta: s}
}

func usageEvent(in, out int) provider.Event {
	return provider.Event{Type: provider.EventUsage, Usage: &schema.Tokens{Input: iptr(in), Output: iptr(out)}}
}

// report is a small measured report the proposer renders into the prompt.
func report() insights.Report {
	return insights.Report{Turns: 2, TotalTokens: 1500, BigOutputs: []insights.Output{{Tool: "grep", Tokens: 5000, Seq: 4}}}
}

// TestProposeParsesGroundedAndPrices drives the proposer with a mock provider that
// returns a JSON array (wrapped in prose, to exercise the lenient slice) and a
// usage event: the suggestions parse, are tagged model-authored, and the call is
// priced on the cheap tier.
func TestProposeParsesGroundedAndPrices(t *testing.T) {
	mock := &provider.Mock{
		NameValue: "anthropic",
		Events: []provider.Event{
			textDelta("Here you go:\n[{\"summary\":\"Scope grep\",\"evidence\":\"~5k tokens at #4\"},"),
			textDelta("{\"summary\":\"\",\"evidence\":\"skip me\"}]"),
			usageEvent(800, 60),
		},
	}
	p := New(mock, routing.Default(), "claude-opus-4-8", cost.Embedded())

	got, spent, err := p.Propose(context.Background(), report())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("suggestions = %+v, want 1 (the empty-summary one dropped)", got)
	}
	if got[0].Summary != "Scope grep" || got[0].Source != insights.SourceModel {
		t.Errorf("suggestion = %+v, want Scope grep tagged model", got[0])
	}
	if spent <= 0 {
		t.Errorf("spent = %v, want a positive cheap-tier charge", spent)
	}

	// The pass must resolve and call the cheap tier, not the session's base model.
	reqs := mock.Requests()
	if len(reqs) != 1 || reqs[0].Model != "claude-haiku-4-5" {
		t.Errorf("request model = %v, want the cheap tier claude-haiku-4-5", reqs)
	}
	if reqs[0].Params.MaxTokens != maxOutputTokens {
		t.Errorf("MaxTokens = %d, want the bounded %d", reqs[0].Params.MaxTokens, maxOutputTokens)
	}
}

// TestDescribeReturnsUsageEvent asserts the on-demand twin (AS-137) returns the
// same grounded suggestions plus a usage event the caller records to charge the
// session budget, rather than a pre-priced dollar figure.
func TestDescribeReturnsUsageEvent(t *testing.T) {
	mock := &provider.Mock{
		NameValue: "anthropic",
		Events: []provider.Event{
			textDelta("[{\"summary\":\"Scope grep\",\"evidence\":\"~5k tokens at #4\"}]"),
			usageEvent(800, 60),
		},
	}
	p := New(mock, routing.Default(), "claude-opus-4-8", cost.Embedded())

	got, usage, err := p.Describe(context.Background(), report())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(got) != 1 || got[0].Summary != "Scope grep" {
		t.Fatalf("suggestions = %+v, want the single grounded one", got)
	}
	if usage.Kind == "" || usage.Tokens == nil || usage.Tokens.Input == nil || *usage.Tokens.Input != 800 {
		t.Errorf("usage event = %+v, want a recordable usage block carrying the turn's tokens", usage)
	}
	// The usage block must name the cheap tier that served the call so accounting prices it.
	if usage.Provider == nil || usage.Provider.Model != "claude-haiku-4-5" {
		t.Errorf("usage model = %+v, want the cheap tier", usage.Provider)
	}
}

// TestDescribeNoUsageNoEvent asserts a reply with no usage yields the zero block
// (nothing to charge), so the caller appends nothing.
func TestDescribeNoUsageNoEvent(t *testing.T) {
	mock := &provider.Mock{NameValue: "anthropic", Events: []provider.Event{textDelta("[]")}}
	_, usage, err := New(mock, routing.Default(), "claude-opus-4-8", cost.Embedded()).Describe(context.Background(), report())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if usage.Kind != "" {
		t.Errorf("want the zero block when the surface reported no usage, got %+v", usage)
	}
}

// TestProposeGarbledReplyYieldsNothing asserts a non-JSON reply degrades to no
// suggestions (never an error): the measured dashboard stands on its own.
func TestProposeGarbledReplyYieldsNothing(t *testing.T) {
	mock := &provider.Mock{NameValue: "anthropic", Events: []provider.Event{textDelta("sorry, no idea")}}
	got, _, err := New(mock, routing.Default(), "claude-opus-4-8", cost.Embedded()).Propose(context.Background(), report())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no suggestions from a garbled reply, got %+v", got)
	}
}

// TestProposeStreamErrorSurfaces asserts a failed turn surfaces an error so the
// writer can skip the layer (rather than silently charging or hanging).
func TestProposeStreamErrorSurfaces(t *testing.T) {
	mock := &provider.Mock{NameValue: "anthropic", StreamErr: context.DeadlineExceeded}
	if _, _, err := New(mock, routing.Default(), "claude-opus-4-8", cost.Embedded()).Propose(context.Background(), report()); err == nil {
		t.Error("want an error when the stream fails")
	}
}
