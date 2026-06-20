// Command bench runs Agent Smith's cost/speed benchmark suite (AS-030, PRD D5)
// and emits JSON + Markdown reports of the §6 metrics for the Smith path and a
// frozen naive baseline.
//
// Usage:
//
//	go run ./cmd/bench                       # offline scripted suite (deterministic, no network)
//	go run ./cmd/bench -out .cache/bench     # also write report.json + report.md there
//	go run ./cmd/bench -provider anthropic -model claude-opus-4-8   # real run, on demand
//	go run ./cmd/bench -compare base.json    # run, then flag regressions vs a saved report
//
// By design it is NOT a per-PR CI gate (D5/AS-030): the default offline mode
// validates fixtures deterministically; real runs are stochastic and reported,
// never failed on. -compare flags large directional changes (RegressionThreshold)
// for a human to review.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tonitienda/agent-smith/internal/benchmark"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/smithapp"
)

// defaultOfflineModel is a real, priced model id so the offline suite produces
// non-zero, deterministic dollar figures from the embedded pricing table. No
// network call is made in offline mode — only the price table is consulted.
const defaultOfflineModel = "claude-opus-4-8"

func main() {
	var (
		providerName = flag.String("provider", "", "real provider to run against (e.g. anthropic, openai); empty = offline scripted suite")
		model        = flag.String("model", "", "model id (defaults to a priced model offline, or the configured chat model for real runs)")
		out          = flag.String("out", "", "directory to write report.json and report.md into (also printed to stdout)")
		compare      = flag.String("compare", "", "path to a baseline report.json to flag regressions against")
		output       = flag.Int("output-tokens", 50, "offline mode: fixed output tokens charged per turn")
	)
	flag.Parse()

	if err := run(*providerName, *model, *out, *compare, *output); err != nil {
		fmt.Fprintln(os.Stderr, "bench: "+err.Error())
		os.Exit(1)
	}
}

func run(providerName, model, out, compare string, outputTokens int) error {
	runner, err := buildRunner(providerName, model, outputTokens)
	if err != nil {
		return err
	}

	rep, err := runner.Run(context.Background())
	if err != nil {
		return err
	}

	fmt.Print(rep.Markdown())

	if out != "" {
		if err := writeReports(out, rep); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "bench: wrote %s/report.json and report.md\n", out)
	}

	if compare != "" {
		regs, err := compareTo(compare, rep)
		if err != nil {
			return err
		}
		printRegressions(regs)
	}
	return nil
}

// buildRunner wires the offline scripted suite or, when a provider is named, a
// real provider run.
func buildRunner(providerName, model string, outputTokens int) (benchmark.Runner, error) {
	pricing := cost.Embedded()
	r := benchmark.Runner{
		Harnesses: []benchmark.Harness{benchmark.SmithHarness{}, benchmark.NaiveHarness{}},
		Tasks:     benchmark.Suite(),
		Pricing:   pricing,
	}

	if providerName == "" {
		if model == "" {
			model = defaultOfflineModel
		}
		r.Provider = benchmark.ScriptedProvider(model, outputTokens)
		r.Model = model
		r.ProviderName = "mock"
		return r, nil
	}

	rt := smithapp.NewRuntime(smithapp.RuntimeConfig{})
	providers := rt.Providers()
	prov, ok := providers[providerName]
	if !ok {
		return benchmark.Runner{}, fmt.Errorf("unknown provider %q", providerName)
	}
	if model == "" {
		_, model = rt.SelectProviderModel(pricing, providers, "")
	}
	r.Provider = benchmark.RealProvider(prov)
	r.Model = model
	r.ProviderName = providerName
	return r, nil
}

func writeReports(dir string, rep benchmark.Report) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := rep.JSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), []byte(rep.Markdown()), 0o644)
}

func compareTo(path string, current benchmark.Report) ([]benchmark.Regression, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var baseline benchmark.Report
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return benchmark.Compare(baseline, current), nil
}

func printRegressions(regs []benchmark.Regression) {
	if len(regs) == 0 {
		fmt.Println("\nNo regressions past the threshold.")
		return
	}
	fmt.Printf("\n## Regressions (%d)\n\n", len(regs))
	for _, r := range regs {
		fmt.Printf("- %s/%s [%s]: %s\n", r.Task, r.Harness, r.Kind, r.Detail)
	}
}
