package benchmark

import (
	"context"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
)

// benchModel is a real, priced model id so the embedded pricing table gives the
// suite non-zero, deterministic dollar figures. The exact rate is irrelevant to
// the assertions, which compare relative values.
const benchModel = "claude-opus-4-8"

func offlineRunner() Runner {
	return Runner{
		Harnesses:    []Harness{SmithHarness{}, NaiveHarness{}},
		Tasks:        Suite(),
		Provider:     ScriptedProvider(benchModel, 50),
		Model:        benchModel,
		ProviderName: "mock",
		Pricing:      cost.Embedded(),
	}
}

func runResult(t *testing.T, rep Report, task, harness string) RunResult {
	t.Helper()
	for _, r := range rep.Runs {
		if r.Task == task && r.Harness == harness {
			return r
		}
	}
	t.Fatalf("no run for task %q harness %q", task, harness)
	return RunResult{}
}

// The suite runs offline and every fixture's scripted agent completes its task on
// both harnesses, with priced, non-zero cost.
func TestSuiteCompletesOffline(t *testing.T) {
	rep, err := offlineRunner().Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rep.Runs) != len(Suite())*2 {
		t.Fatalf("expected %d runs, got %d", len(Suite())*2, len(rep.Runs))
	}
	for _, r := range rep.Runs {
		if r.Err != "" {
			t.Errorf("%s/%s errored: %s", r.Task, r.Harness, r.Err)
		}
		if !r.Metrics.Success {
			t.Errorf("%s/%s did not succeed: %s", r.Task, r.Harness, r.Metrics.Detail)
		}
		if !r.Metrics.Priced || r.Metrics.TotalUSD <= 0 {
			t.Errorf("%s/%s expected priced non-zero cost, got priced=%v usd=%v",
				r.Task, r.Harness, r.Metrics.Priced, r.Metrics.TotalUSD)
		}
	}
}

// The naive baseline resends excluded context, so on the bloat fixture it costs
// strictly more than the Smith path — the D5 guardrail in miniature.
func TestNaiveCostsMoreOnBloat(t *testing.T) {
	rep, err := offlineRunner().Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	smith := runResult(t, rep, "context-bloat", "smith").Metrics
	naive := runResult(t, rep, "context-bloat", "naive").Metrics

	if naive.Tokens <= smith.Tokens {
		t.Errorf("naive should send more tokens than smith on bloat: naive=%d smith=%d",
			naive.Tokens, smith.Tokens)
	}
	if naive.TotalUSD <= smith.TotalUSD {
		t.Errorf("naive should cost more than smith on bloat: naive=%v smith=%v",
			naive.TotalUSD, smith.TotalUSD)
	}
	// Smith dropped the excluded block, so its live-context ratio is below full.
	if smith.LiveContextPct >= 100 {
		t.Errorf("smith live-context%% should be < 100 after exclusion, got %.1f", smith.LiveContextPct)
	}
}

// A deliberate context-bloat regression is visible in Compare: re-running with no
// exclusion (the regression) inflates Smith's cost past the threshold and Compare
// flags it.
func TestCompareDetectsContextBloatRegression(t *testing.T) {
	baseline, err := offlineRunner().Run(context.Background())
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Regress: drop the exclusion so the Smith path now carries the bloat too.
	r := offlineRunner()
	tasks := append([]Task(nil), r.Tasks...)
	for i := range tasks {
		if tasks[i].ID == "context-bloat" {
			tasks[i].Seed = tasks[i].Seed[:1] // keep the bloat block, drop the exclusion
		}
	}
	r.Tasks = tasks
	current, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}

	regs := Compare(baseline, current)
	if !hasRegression(regs, "context-bloat", "smith", "context") {
		t.Fatalf("expected a context regression on context-bloat/smith, got %+v", regs)
	}
}

func hasRegression(regs []Regression, task, harness, kind string) bool {
	for _, r := range regs {
		if r.Task == task && r.Harness == harness && r.Kind == kind {
			return true
		}
	}
	return false
}

// Reports serialize to JSON and render Markdown with a row per run.
func TestReportRendering(t *testing.T) {
	rep, err := offlineRunner().Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := rep.JSON(); err != nil {
		t.Fatalf("json: %v", err)
	}
	md := rep.Markdown()
	for _, want := range []string{"Benchmark report", "Harness: smith", "Harness: naive", "context-bloat"} {
		if !contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
