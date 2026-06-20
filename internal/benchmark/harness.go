package benchmark

import (
	"context"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// ScriptedTurn is one provider turn the offline suite replays: the events an
// agent would emit that turn (text and/or tool calls), built with the
// provider.*Turn helpers. The scripted provider appends a usage event sized to
// the turn's input context, so cost and the Smith-vs-naive gap track context
// size deterministically with no network.
type ScriptedTurn = []provider.Event

// recorder collects per-turn timing for a single run. The provider records the
// time-to-first-event and total duration of each turn it serves; the runner then
// derives TTFT (first turn) and the median turn latency.
type recorder struct {
	mu        sync.Mutex
	ttfts     []time.Duration
	durations []time.Duration
}

func (r *recorder) add(ttft, dur time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ttfts = append(r.ttfts, ttft)
	r.durations = append(r.durations, dur)
}

// timings returns the first turn's TTFT and the median turn latency.
func (r *recorder) timings() (ttft, median time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ttfts) > 0 {
		ttft = r.ttfts[0]
	}
	return ttft, medianDuration(append([]time.Duration(nil), r.durations...))
}

// timingStream wraps a Stream to record its first-event and close timings into a
// recorder. It is the seam that captures TTFT and turn latency for both the
// scripted and real providers.
type timingStream struct {
	provider.Stream
	rec   *recorder
	start time.Time
	first time.Time
	seen  bool
}

func (s *timingStream) Next() bool {
	ok := s.Stream.Next()
	if ok && !s.seen {
		s.seen = true
		s.first = time.Now()
	}
	return ok
}

func (s *timingStream) Close() error {
	end := time.Now()
	// A stream that produced no event has no first token: report TTFT as 0
	// rather than the full duration, which would be a false latency reading.
	var ttft time.Duration
	if s.seen {
		ttft = s.first.Sub(s.start)
	}
	s.rec.add(ttft, end.Sub(s.start))
	return s.Stream.Close()
}

// scriptedProvider replays a task's Turns in order, one per Stream call, and
// injects a usage event sized to the request context so cost is a deterministic
// function of the window. When the script is exhausted it ends the turn cleanly,
// so a loop that takes an extra turn (e.g. after a tool result) still terminates.
type scriptedProvider struct {
	model        string
	turns        []ScriptedTurn
	outputTokens int
	rec          *recorder

	mu  sync.Mutex
	idx int
}

func (p *scriptedProvider) Name() string { return "mock" }

func (p *scriptedProvider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	var events []provider.Event
	if p.idx < len(p.turns) {
		events = append(events, p.turns[p.idx]...)
	} else {
		events = provider.TextTurn("done", provider.StopEndTurn)
	}
	p.idx++
	p.mu.Unlock()

	events = withUsage(events, req, p.outputTokens)
	stream := provider.SliceStream(events, nil)
	return &timingStream{Stream: stream, rec: p.rec, start: time.Now()}, nil
}

// withUsage inserts a usage event (input sized to the request context, output a
// fixed cost) just before the turn stop. The cost engine prices it by the turn's
// model, which the loop defaults to the request model, so no model need be set on
// the event. A turn with no stop is returned unchanged.
func withUsage(events []provider.Event, req provider.Request, output int) []provider.Event {
	input := cost.EstimateContextTokens(req.Context)
	usage := provider.Event{
		Type: provider.EventUsage,
		Usage: &schema.Tokens{
			Input:  intPtr(input),
			Output: intPtr(output),
		},
	}
	for i, e := range events {
		if e.Type == provider.EventTurnStop {
			out := make([]provider.Event, 0, len(events)+1)
			out = append(out, events[:i]...)
			out = append(out, usage)
			out = append(out, events[i:]...)
			return out
		}
	}
	return events
}

func intPtr(n int) *int { return &n }

// ScriptedProvider builds the offline ProviderFor: a deterministic provider that
// replays each task's Turns and prices usage by context size. outputTokens is the
// fixed per-turn output charge. This is the default suite driver — no network, no
// model calls.
func ScriptedProvider(model string, outputTokens int) ProviderFor {
	return func(t Task, rec *recorder) (provider.Provider, error) {
		return &scriptedProvider{
			model:        model,
			turns:        t.Turns,
			outputTokens: outputTokens,
			rec:          rec,
		}, nil
	}
}

// RealProvider wraps a real provider with timing capture for on-demand runs
// against an actual model. The task's Turns are ignored — the model drives
// itself — so results are stochastic and reported, never asserted on.
func RealProvider(p provider.Provider) ProviderFor {
	return func(_ Task, rec *recorder) (provider.Provider, error) {
		return &timedProvider{Provider: p, rec: rec}, nil
	}
}

type timedProvider struct {
	provider.Provider
	rec *recorder
}

func (p *timedProvider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	start := time.Now()
	s, err := p.Provider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	return &timingStream{Stream: s, rec: p.rec, start: start}, nil
}
