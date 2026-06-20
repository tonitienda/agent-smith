// Package benchmark is Agent Smith's cost/speed guardrail suite (AS-030, PRD D5,
// §6). D5 makes "cheaper/faster than a naive harness on the same model" an
// internal design criterion measured on a benchmark suite — not a marketing
// claim — and §6 adds the guardrails that task success must not regress and
// /clean//tidy must lose no data. None of that is measurable without a repeatable
// runner, so this package provides one.
//
// A Runner drives a fixed suite of fixture Tasks through one or more Harnesses
// and emits a Report (JSON + Markdown) of the §6 metrics: cost per completed
// task, task success, time-to-first-token, median turn latency, and end-of-
// session live-context %. The two built-in harnesses are the Smith path (the
// real loop with context projection) and a frozen naive baseline (the same loop,
// same provider/model and tools, but no Smith context management) so the D5
// comparison is apples-to-apples.
//
// Default tests are deterministic and offline: the suite is driven by a scripted
// provider (no network, no model calls), so cost and context are a pure function
// of the fixtures. Real provider runs are on demand (see cmd/bench), never a
// per-PR CI gate — stochastic results are reported, not failed on (Compare flags
// large directional changes instead).
package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/schema"
)

// Task is one fixture in the suite: a prompt the agent is asked to complete in a
// scratch workspace, judged by inspecting the resulting files. Determinism comes
// from Turns — the fixed agent behavior the scripted provider replays — so a run
// measures the harness, not the model. Seed pre-loads log events (e.g. prior
// context the Smith path can exclude) to exercise the Smith-vs-naive gap.
type Task struct {
	// ID is the stable task identifier shown in reports.
	ID string
	// Prompt is the user request that opens the run.
	Prompt string
	// Setup seeds the scratch workspace before the run (relative paths under dir).
	// nil leaves an empty workspace.
	Setup func(dir string) error
	// Seed is appended to the log before the prompt (already-present context such
	// as a large tool result plus an exclusion event). Empty for a clean session.
	Seed []schema.Block
	// Turns is the scripted agent behavior, one provider turn per element, replayed
	// in order by the offline provider. Ignored by a real provider, which drives
	// itself.
	Turns []ScriptedTurn
	// Check judges success from the resulting workspace, returning a short reason.
	Check func(dir string) (ok bool, detail string)
}

// Metrics are the §6 measurements for one task run on one harness.
type Metrics struct {
	Success           bool          `json:"success"`
	Detail            string        `json:"detail,omitempty"`
	Iterations        int           `json:"iterations"`
	Tokens            int           `json:"tokens"`
	TotalUSD          float64       `json:"total_usd"`
	Priced            bool          `json:"priced"`
	TTFT              time.Duration `json:"ttft_ns"`
	MedianTurnLatency time.Duration `json:"median_turn_latency_ns"`
	// LiveContextPct is the end-of-session live/rendered ratio as a percentage:
	// 100 when nothing was excluded, lower when the harness dropped context.
	LiveContextPct float64 `json:"live_context_pct"`
}

// RunResult pairs a task/harness with its metrics (or the error that ended it).
type RunResult struct {
	Task    string  `json:"task"`
	Harness string  `json:"harness"`
	Metrics Metrics `json:"metrics"`
	Err     string  `json:"error,omitempty"`
}

// Harness is a context-management policy under test. Name labels it in reports;
// projector returns the loop's context assembler (nil = Smith's default
// projection).
type Harness interface {
	Name() string
	projector() loop.Projector
}

// SmithHarness is the real Smith path: the loop with its default projection
// (exclusions honored, reasoning-replay filtered).
type SmithHarness struct{}

func (SmithHarness) Name() string              { return "smith" }
func (SmithHarness) projector() loop.Projector { return nil }

// NaiveHarness is the frozen D5 baseline: the same loop, provider/model and
// tools, but no Smith context management — every rendered block is sent every
// turn, exclusion events ignored. Versioned with the suite; do not "improve" it,
// it is the thing Smith is measured against.
type NaiveHarness struct{}

func (NaiveHarness) Name() string { return "naive" }
func (NaiveHarness) projector() loop.Projector {
	return func(events []schema.Block) []schema.Block {
		// Render the window with no context management: take every model-facing
		// block the projection would render, live or excluded. projection.Blocks
		// already drops control/usage events that are not part of the context.
		proj := projection.Project(events, projection.Options{})
		blocks := proj.Blocks()
		out := make([]schema.Block, 0, len(blocks))
		for _, b := range blocks {
			out = append(out, b.Block)
		}
		return out
	}
}

// ProviderFor builds the provider for one task run, given a per-run timing
// recorder. The offline suite returns a scripted provider (deterministic); a real
// run returns a timing-wrapped real provider. model is the model ID reported on
// usage events and passed to the loop.
type ProviderFor func(t Task, rec *recorder) (provider.Provider, error)

// Runner drives Tasks through Harnesses and prices the result with Pricing.
type Runner struct {
	Harnesses []Harness
	Tasks     []Task
	// Provider builds the provider per run. Required.
	Provider ProviderFor
	// Model is the model ID for the loop and usage pricing.
	Model string
	// ProviderName labels the report (e.g. "mock", "anthropic").
	ProviderName string
	// Pricing prices usage; nil reports tokens with zero, unpriced dollars.
	Pricing *cost.Table
	// MaxIterations caps the loop per task (0 = loop default).
	MaxIterations int
}

// Run executes every (harness, task) pair and returns a Report. A task error is
// captured on its RunResult rather than aborting the suite, so one broken fixture
// does not hide the rest.
func (r Runner) Run(ctx context.Context) (Report, error) {
	if r.Provider == nil {
		return Report{}, errors.New("benchmark: Runner.Provider is required")
	}
	if r.Model == "" {
		return Report{}, errors.New("benchmark: Runner.Model is required")
	}
	rep := Report{
		Provider:    r.ProviderName,
		Model:       r.Model,
		GeneratedAt: time.Now().UTC(),
	}
	for _, h := range r.Harnesses {
		for _, t := range r.Tasks {
			// Abort early on cancellation rather than running every remaining pair
			// against a dead context and filling the report with cancel errors.
			if err := ctx.Err(); err != nil {
				return rep, err
			}
			m, err := r.runOne(ctx, h, t)
			res := RunResult{Task: t.ID, Harness: h.Name(), Metrics: m}
			if err != nil {
				res.Err = err.Error()
			}
			rep.Runs = append(rep.Runs, res)
		}
	}
	return rep, nil
}

// runOne runs a single task on a single harness in an isolated workspace.
func (r Runner) runOne(ctx context.Context, h Harness, t Task) (Metrics, error) {
	dir, err := os.MkdirTemp("", "smith-bench-")
	if err != nil {
		return Metrics{}, fmt.Errorf("workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if t.Setup != nil {
		if err := t.Setup(dir); err != nil {
			return Metrics{}, fmt.Errorf("setup: %w", err)
		}
	}

	log := eventlog.New()
	for _, b := range t.Seed {
		if _, err := log.Append(b); err != nil {
			return Metrics{}, fmt.Errorf("seed: %w", err)
		}
	}

	fsTools, err := builtin.NewFS(dir)
	if err != nil {
		return Metrics{}, fmt.Errorf("fs tools: %w", err)
	}
	reg := tool.NewRegistry()
	for _, tl := range fsTools.Tools() {
		if err := reg.Register(tl); err != nil {
			return Metrics{}, fmt.Errorf("register %s: %w", tl.Def().Name, err)
		}
	}
	rt := tool.NewRuntime(reg, log)

	rec := &recorder{}
	prov, err := r.Provider(t, rec)
	if err != nil {
		return Metrics{}, fmt.Errorf("provider: %w", err)
	}

	opts := []loop.Option{loop.WithProjector(h.projector())}
	if r.MaxIterations > 0 {
		opts = append(opts, loop.WithMaxIterations(r.MaxIterations))
	}
	eng, err := loop.New(prov, log, rt, reg, r.Model, opts...)
	if err != nil {
		return Metrics{}, fmt.Errorf("loop: %w", err)
	}

	res, runErr := eng.Run(ctx, t.Prompt)

	m := Metrics{Iterations: res.Iterations}
	m.TTFT, m.MedianTurnLatency = rec.timings()
	r.priceInto(&m, log.Events())
	m.LiveContextPct = liveContextPct(log.Events(), r.Model)
	if t.Check != nil {
		m.Success, m.Detail = t.Check(dir)
	}
	if runErr != nil {
		return m, runErr
	}
	return m, nil
}

// priceInto fills the cost/token metrics from the session's usage events.
func (r Runner) priceInto(m *Metrics, events []schema.Block) {
	s := cost.Summarize(events, r.Pricing)
	m.Tokens = s.Total.Total()
	m.TotalUSD = s.TotalUSD
	m.Priced = s.AllPriced && len(s.Turns) > 0
}

// liveContextPct is the end-of-session live/rendered ratio as a percentage.
func liveContextPct(events []schema.Block, model string) float64 {
	proj := projection.Project(events, projection.Options{TargetModel: model})
	total := proj.Len()
	if total == 0 {
		return 100
	}
	return float64(proj.LiveLen()) / float64(total) * 100
}

// fixtureRoot resolves a path under a workspace dir (helper for fixtures).
func fixtureRoot(dir, rel string) string { return filepath.Join(dir, rel) }

// medianDuration returns the median of ds (0 for an empty slice). ds is sorted
// in place.
func medianDuration(ds []time.Duration) time.Duration {
	n := len(ds)
	if n == 0 {
		return 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	if n%2 == 1 {
		return ds[n/2]
	}
	return (ds[n/2-1] + ds[n/2]) / 2
}
