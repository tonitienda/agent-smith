package main

import (
	"fmt"
	"io"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/redaction"
)

// applyRedaction wires capture-time redaction (AS-115) onto the session log when
// the `redaction` config section turns it on. It is off by default — best-effort
// defense-in-depth, not the erasure guarantee — so a session without the setting
// is unaffected. A malformed extra pattern is skipped with a warning, never
// fatal (PRD D2). The typed view (AS-093) owns the `redaction.*` paths.
func applyRedaction(cfg *config.Config, log *eventlog.Log, stderr io.Writer) {
	redactCfg, warns := redaction.ConfigFrom(cfg)
	for _, w := range warns {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if !redactCfg.Enabled {
		return
	}
	red, buildWarns := redaction.Build(redactCfg)
	for _, w := range buildWarns {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	log.SetRedactor(red)
}
