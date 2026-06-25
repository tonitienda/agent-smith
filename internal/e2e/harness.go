// Package e2e is the offline end-to-end regression suite (AS-134). It drives a
// full Smith session — the same loop, tool runtime, append-only log, cost
// accounting, and sub-agent delegation the executable faces wire — against the
// recorded vendor simulators from AS-133, so expensive whole-session
// regressions are caught in CI with no vendor API keys and no live network.
//
// A scenario scripts an ordered list of provider Exchanges (built with the SSE
// helpers in sse.go), seeds a temp working directory and a disk-backed session,
// runs scripted prompts through the engine, and asserts on the resulting
// transcript, the face-agnostic UIEvent stream the TUI consumes, token/cost
// accounting, the per-child delegation ledger, and the on-disk JSONL — including
// that resuming the session from disk reprojects deterministically and never
// mutates a previously written event.
//
// The suite is plain Go tests, so it runs in `make test` with the rest of the
// gate; see docs/testing/offline-e2e-suite.md for how to evolve a scenario and
// tell an intended schema change from a regression.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/delegate"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/provider/anthropic"
	"github.com/tonitienda/agent-smith/internal/provider/conformance"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/schema"
)

// scenarioModel is the model every scenario issues turns against. It is a
// real priced model (claude-sonnet-4-*) so cost accounting produces non-zero
// dollar totals the assertions can check; the recorded server makes the choice
// free of any live call.
const scenarioModel = "claude-sonnet-4-6"

// sseHeader is the response header the simulator returns for every streamed
// turn, marking the body as a Server-Sent Events stream the adapter parses.
var sseHeader = map[string]string{"Content-Type": "text/event-stream"}

// turn is one scripted provider response in a scenario, in order. Build the body
// with textTurn or toolTurn. BodyContains optionally asserts the request the
// adapter serialized for this turn carried the given substrings (e.g. a tool
// result fed back, the model id), so the scenario guards request serialization
// as well as response handling.
type turn struct {
	body         []byte
	bodyContains []string
}

// answer is a convenience for a final end_turn text turn.
func answer(id, text string) turn {
	return turn{body: textTurn(id, scenarioModel, text, 40, 12)}
}

// tools is a convenience for a tool_use turn requesting the given calls.
func tools(id string, calls ...toolUse) turn {
	return turn{body: toolTurn(id, scenarioModel, calls, 30, 20)}
}

// Harness is a single offline scenario: a temp project, a disk-backed session, a
// recorded simulator pre-loaded with the scenario's turns, and the wired engine.
// Run drives a prompt to completion; the captured UIEvents, the live log, and the
// pricing table back the assertions.
type Harness struct {
	t       *testing.T
	dir     string
	store   *session.Store
	sess    *session.Session
	server  *conformance.RecordedServer
	engine  *loop.Engine
	pricing *cost.Table

	// UI captures every face-agnostic UIEvent the loop emitted, in order — the
	// same stream the TUI (AS-021/024) renders into tool cards, permission state,
	// and turn lifecycle. Asserting on it verifies the model layer without a
	// terminal (AS-134).
	UI []loop.UIEvent
}

// Option configures a Harness before its engine is built.
type Option func(*config)

type config struct {
	files      map[string]string
	permission tool.PermissionFunc
	delegation bool
	engineOpts []loop.Option
}

// WithFile seeds a file in the scenario working directory before the run, so a
// read/edit tool call has real content to act on.
func WithFile(path, content string) Option {
	return func(c *config) { c.files[path] = content }
}

// WithPermission installs a permission gate, so a scenario can script a denied
// tool call and assert the model recovers from the error result.
func WithPermission(fn tool.PermissionFunc) Option {
	return func(c *config) { c.permission = fn }
}

// WithDelegation registers the `task` tool backed by a real delegate.Spawner, so
// a scenario can exercise parent→child fan-out and the per-child cost ledger
// (AS-046/AS-119/AS-120). The child sessions draw their turns from the same
// recorded simulator, in script order.
func WithDelegation() Option {
	return func(c *config) { c.delegation = true }
}

// WithEngineOption threads an extra loop.Option (e.g. WithBudget) into the
// scenario engine.
func WithEngineOption(opt loop.Option) Option {
	return func(c *config) { c.engineOpts = append(c.engineOpts, opt) }
}

// New builds a scenario harness whose simulator serves turns in order. Close is
// registered with t.Cleanup. It fails the test at construction on any wiring
// error, so a scenario body can assume a ready engine.
func New(t *testing.T, turns []turn, opts ...Option) *Harness {
	t.Helper()
	cfg := &config{files: map[string]string{}}
	for _, o := range opts {
		o(cfg)
	}

	dir := t.TempDir()
	for path, content := range cfg.files {
		writeSeed(t, dir, path, content)
	}

	server := conformance.NewRecordedServer(exchanges(turns)...)
	t.Cleanup(server.Close)

	providers := map[string]provider.Provider{
		"anthropic": anthropic.New("test-key", anthropic.WithBaseURL(server.URL)),
	}

	store, err := session.NewStore(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("e2e: new store: %v", err)
	}
	sess, err := store.Create("e2e scenario")
	if err != nil {
		t.Fatalf("e2e: create session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Log.Close() })

	pricing := cost.Embedded()

	reg, err := buildTools(dir, cfg, store, providers, sess, pricing)
	if err != nil {
		t.Fatalf("e2e: build tools: %v", err)
	}

	var rtOpts []tool.Option
	if cfg.permission != nil {
		rtOpts = append(rtOpts, tool.WithPermission(cfg.permission))
	}
	rt := tool.NewRuntime(reg, sess.Log, rtOpts...)

	h := &Harness{t: t, dir: dir, store: store, sess: sess, server: server, pricing: pricing}
	engOpts := append([]loop.Option{loop.WithObserver(func(ev loop.UIEvent) { h.UI = append(h.UI, ev) })}, cfg.engineOpts...)
	eng, err := loop.New(providers["anthropic"], sess.Log, rt, reg, scenarioModel, engOpts...)
	if err != nil {
		t.Fatalf("e2e: build engine: %v", err)
	}
	h.engine = eng
	return h
}

// buildTools assembles the scenario's tool registry: the built-in file and shell
// tools rooted at the working directory, plus the `task` tool when delegation is
// enabled. Child agents reuse the file/shell set without `task`, so delegation
// never recurses.
func buildTools(dir string, cfg *config, store *session.Store, providers map[string]provider.Provider, sess *session.Session, pricing *cost.Table) (*tool.Registry, error) {
	fileShell := func() (*tool.Registry, error) {
		reg := tool.NewRegistry()
		fs, err := builtin.NewFS(dir)
		if err != nil {
			return nil, err
		}
		for _, tl := range fs.Tools() {
			if err := reg.Register(tl); err != nil {
				return nil, err
			}
		}
		shell, err := builtin.NewShell(dir)
		if err != nil {
			return nil, err
		}
		if err := reg.Register(shell); err != nil {
			return nil, err
		}
		return reg, nil
	}

	reg, err := fileShell()
	if err != nil {
		return nil, err
	}
	if cfg.delegation {
		spawner := delegate.New(store, providers, fileShell, func() delegate.Parent {
			return delegate.Parent{
				Log:            sess.Log,
				SessionID:      sess.ID,
				ProvName:       "anthropic",
				Model:          scenarioModel,
				Permission:     cfg.permission,
				Router:         routing.Policy{},
				Pricing:        pricing,
				ChildBudgetUSD: 0, // bounded by max-iterations; the ledger still itemizes spend
			}
		})
		if err := reg.Register(builtin.NewTask(spawner)); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// Run drives one prompt through the engine to a stop condition and returns the
// loop Result. It fails the test on an unexpected error.
func (h *Harness) Run(prompt string) loop.Result {
	h.t.Helper()
	res, err := h.engine.Run(context.Background(), prompt)
	if err != nil {
		h.t.Fatalf("e2e: run %q: %v", prompt, err)
	}
	return res
}

// AssertSimulatorDrained fails the test if any scripted turn went unrequested or
// a request mismatched, so a scenario that diverges from its script (an extra
// turn, a dropped tool result) fails with an actionable diff.
func (h *Harness) AssertSimulatorDrained() {
	h.t.Helper()
	h.server.AssertConsumed(h.t)
}

// Events returns a snapshot of the live session log.
func (h *Harness) Events() []schema.Block { return h.sess.Log.Events() }

// Cost prices the live log, including any delegated child spend rolled up as a
// sidechain (AS-046/AS-120).
func (h *Harness) Cost() cost.Summary {
	return cost.Summarize(h.sess.Log.Events(), h.pricing)
}

// Reopen closes the live log and reopens the session from disk, returning the
// reloaded log's events. It is how a scenario asserts that resume/rehydration
// reprojects the same history the live run produced (deterministic projection,
// no mutation of previously written events).
func (h *Harness) Reopen() []schema.Block {
	h.t.Helper()
	if err := h.sess.Log.Close(); err != nil {
		h.t.Fatalf("e2e: close log: %v", err)
	}
	reopened, err := h.store.Open(h.sess.ID)
	if err != nil {
		h.t.Fatalf("e2e: reopen session: %v", err)
	}
	h.t.Cleanup(func() { _ = reopened.Log.Close() })
	return reopened.Log.Events()
}

// exchanges turns the scenario's scripted turns into recorded-server Exchanges.
func exchanges(turns []turn) []conformance.Exchange {
	out := make([]conformance.Exchange, len(turns))
	for i, tn := range turns {
		out[i] = conformance.Exchange{
			Path:         "/v1/messages",
			BodyContains: tn.bodyContains,
			Status:       200,
			Header:       sseHeader,
			Body:         tn.body,
		}
	}
	return out
}

// writeSeed creates a scenario input file under dir, failing the test on error.
func writeSeed(t *testing.T, dir, path, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("e2e: seed dir %s: %v", path, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("e2e: seed file %s: %v", path, err)
	}
}
