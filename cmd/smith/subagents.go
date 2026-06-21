package main

import (
	"fmt"
	"io"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/factdetector"
	"github.com/tonitienda/agent-smith/internal/subagent"
)

// buildSubAgents constructs the sub-agent registry every face installs (AS-107):
// it registers the built-in system sub-agents (AS-044) and applies the
// `subagents.<name>` config overlay (Appendix C.3), then returns the registry and
// the in-memory insights store findings record into — the seam /insights (AS-045)
// will read. A config entry naming an unknown sub-agent, or an unparsable
// schedule, is surfaced to stderr as a warning rather than failing startup, the
// same tolerate-but-warn ethos as the hook and budget loaders.
//
// factdetector wiring (AS-048): a nil Resolve falls back to the project-root
// memory file (factdetector.DefaultTarget), and a single in-memory ledger is
// shared across the process's sessions so dismissals and the precision tally
// accumulate while each session still gets its own (stateless) detector instance.
// Persisting that ledger across process restarts and a memory/skill-aware
// save-target resolver are precision/durability follow-ons (AS-108).
func buildSubAgents(cfg *config.Config, stderr io.Writer) (*subagent.Registry, subagent.Store, error) {
	reg := subagent.NewRegistry()
	if err := reg.Register(factdetector.Factory(nil, factdetector.NewMemLedger())); err != nil {
		return nil, nil, fmt.Errorf("register sub-agent: %w", err)
	}
	// Guard the typed-nil here rather than relying on Load's nil check: a nil
	// *config.Config passed through the configReader interface is a non-nil
	// interface value, so Load would dereference it. With no config the built-ins
	// run on their manifest defaults.
	if cfg != nil {
		warns, err := reg.Load(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("load sub-agent config: %w", err)
		}
		for _, w := range warns {
			_, _ = fmt.Fprintf(stderr, "warning: %s\n", w)
		}
	}
	return reg, subagent.NewMemStore(), nil
}
