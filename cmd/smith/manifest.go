package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/manifest"
	"github.com/tonitienda/agent-smith/internal/otelexport"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/version"
)

// buildManifest derives the run manifest for sess from its current log and the
// sanitized effective config (AS-055). It is pure apart from reading the log; the
// caller persists it. pricing may be nil (token counts stay exact, dollars read
// unknown).
func buildManifest(sess *session.Session, cfg *config.Config, pricing *cost.Table) manifest.Manifest {
	events := sess.Log.Events()
	return manifest.Build(manifest.Input{
		SessionID:     sess.ID,
		ProjectPath:   sess.Metadata.ProjectPath,
		Parent:        sess.Metadata.Parent,
		BinaryVersion: version.String(),
		CreatedAt:     sess.Metadata.CreatedAt,
		GeneratedAt:   time.Now().UTC(),
		Events:        events,
		Cost:          cost.Summarize(events, pricing),
		Config:        effectiveConfigMap(cfg),
	})
}

// effectiveConfigMap flattens the layered config's effective values into the
// dotted-path map the manifest snapshots. The manifest package sanitizes it
// (drops secret-looking keys) before persisting, so this passes the raw view.
func effectiveConfigMap(cfg *config.Config) map[string]any {
	if cfg == nil {
		return nil
	}
	out := map[string]any{}
	for _, e := range cfg.Effective() {
		out[e.Path] = e.Value
	}
	return out
}

// persistRunArtifacts writes the run manifest next to the session log and, when
// telemetry is configured (off by default), exports the session's OpenTelemetry
// trace. Both are best-effort observability side channels: a failure is reported
// on stderr but never fails the run that produced the log (AS-055, PRD §7.23).
func persistRunArtifacts(ctx context.Context, sess *session.Session, cfg *config.Config, pricing *cost.Table, stderr io.Writer) {
	m := buildManifest(sess, cfg, pricing)
	if err := manifest.Write(sess.Dir, m); err != nil && stderr != nil {
		_, _ = fmt.Fprintf(stderr, "warning: write run manifest: %v\n", err)
	}
	exportTelemetry(ctx, sess, cfg, pricing, stderr)
}

// exportTelemetry ships the session trace to the configured OTLP endpoint when
// export is enabled. A disabled config is a silent no-op.
func exportTelemetry(ctx context.Context, sess *session.Session, cfg *config.Config, pricing *cost.Table, stderr io.Writer) {
	// A failed config load (replayRun ignores the error) leaves cfg nil; a nil
	// *config.Config in the configReader interface would panic in Decode. No config
	// means no endpoint, so treat it as export disabled.
	if cfg == nil {
		return
	}
	telemetry, warns := otelexport.ConfigFrom(cfg)
	if stderr != nil {
		for _, w := range warns {
			_, _ = fmt.Fprintf(stderr, "warning: %s\n", w)
		}
	}
	if !otelexport.Enabled(telemetry) {
		return
	}
	events := sess.Log.Events()
	payload := otelexport.BuildTrace(sess.ID, events, cost.Summarize(events, pricing))
	if err := otelexport.Export(ctx, telemetry, payload); err != nil && stderr != nil {
		_, _ = fmt.Fprintf(stderr, "warning: export OpenTelemetry trace: %v\n", err)
	}
}
