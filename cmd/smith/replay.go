package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/manifest"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/schema"
)

// replayCommand re-renders a persisted session from its append-only log (AS-055,
// PRD §7.23). It is re-display, not re-execution: no provider or tool is invoked,
// so a session replays fully offline with no API keys present. It also rebuilds
// the run manifest for sessions that never wrote one (e.g. a TUI session), and can
// export the session's OpenTelemetry trace with --otel.
func replayCommand() *cli.Command {
	var otel bool
	return &cli.Command{
		Name:          "replay",
		Summary:       "Re-render a stored session from its log (re-display, not re-execution)",
		Usage:         "<session>",
		Scriptability: command.Scriptable.String(),
		Reason:        "renders recorded session data; it never calls a provider or runs a tool",
		OutputSchema:  "text: manifest header + transcript; json: {manifest, blocks[]}",
		Examples: []string{
			"smith replay sess_20260624T...",
			"smith replay sess_20260624T... --output json",
			"smith replay sess_20260624T... --otel",
		},
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&otel, "otel", false, "export the session's OpenTelemetry trace to the configured collector")
		},
		Run: func(c *cli.Context) error { return replayRun(c, otel) },
	}
}

func replayRun(c *cli.Context, otel bool) error {
	if len(c.Args) != 1 {
		return cli.Usagef("replay: want exactly one session id")
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	store, err := session.NewStore("", wd)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	sess, err := store.Open(c.Args[0])
	if err != nil {
		return err
	}
	defer func() { _ = sess.Log.Close() }()

	pricing, err := sessionPricing(c.Globals.Config)
	if err != nil {
		return fmt.Errorf("load pricing table: %w", err)
	}
	cfg, _ := loadLayeredConfig(c.Globals.Config) // best-effort config snapshot for the manifest

	m := buildManifest(sess, cfg, pricing)
	// Self-heal: a session that never ran through a manifest-writing face (a TUI
	// session) has no manifest.json; write the freshly derived one so replay leaves
	// a durable manifest alongside the log.
	if _, ok, _ := manifest.Read(sess.Dir); !ok {
		if err := manifest.Write(sess.Dir, m); err != nil && c.Stderr != nil {
			_, _ = fmt.Fprintf(c.Stderr, "warning: write run manifest: %v\n", err)
		}
	}

	if otel {
		exportTelemetry(context.Background(), sess, cfg, pricing, c.Stderr)
	}

	events := sess.Log.Events()
	blocks := projection.Project(events, projection.Options{}).Blocks()

	if c.Globals.Output == cli.OutputJSON {
		return c.WriteJSON(struct {
			Manifest manifest.Manifest  `json:"manifest"`
			Blocks   []projection.Block `json:"blocks"`
		}{Manifest: m, Blocks: blocks})
	}

	var b strings.Builder
	b.WriteString("# replay — re-display of a recorded session, not a re-execution\n\n")
	b.WriteString(manifest.Render(m))
	if t := renderTranscript(blocks); t != "" {
		b.WriteString("\n\n")
		b.WriteString(t)
	}
	return c.Emit(b.String())
}

// renderTranscript renders the projected blocks as a labeled transcript. Dropped
// blocks (excluded by /clean, /compact, replay-scope, phase-scope) are shown with
// their drop reason so the replay reflects exactly what the projection holds.
func renderTranscript(blocks []projection.Block) string {
	var b strings.Builder
	for _, blk := range blocks {
		line := transcriptLine(blk.Block)
		if line == "" {
			continue
		}
		if !blk.Live {
			fmt.Fprintf(&b, "[dropped: %s] %s\n", blk.Reason, line)
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// transcriptLine renders one block as a single transcript line, or "" for blocks
// with no display body. It reads only the persisted bodies — never a provider — so
// it is pure re-display.
func transcriptLine(b schema.Block) string {
	switch b.Kind {
	case schema.KindText:
		if b.Text == nil || b.Text.Text == "" {
			return ""
		}
		return fmt.Sprintf("%s: %s", b.Role, b.Text.Text)
	case schema.KindReasoning:
		if b.Reasoning == nil {
			return ""
		}
		if b.Reasoning.Text != "" {
			return fmt.Sprintf("%s (reasoning): %s", b.Role, b.Reasoning.Text)
		}
		return fmt.Sprintf("%s (reasoning)", b.Role)
	case schema.KindToolCall:
		if b.ToolCall == nil {
			return ""
		}
		if args := strings.TrimSpace(string(b.ToolCall.Arguments)); args != "" {
			return fmt.Sprintf("→ %s %s", b.ToolCall.Name, args)
		}
		return fmt.Sprintf("→ %s", b.ToolCall.Name)
	case schema.KindToolResult:
		return fmt.Sprintf("← %s", toolResultText(b))
	default:
		return ""
	}
}

// toolResultText renders a tool result body for the transcript, preferring the
// captured stdout, then any text parts, then a neutral marker.
func toolResultText(b schema.Block) string {
	if b.ToolResult == nil {
		return "[tool result]"
	}
	if s := strings.TrimSpace(b.ToolResult.Stdout); s != "" {
		return s
	}
	var parts []string
	for _, p := range b.ToolResult.Content {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	if b.ToolResult.IsError {
		return "[tool error]"
	}
	return "[tool result]"
}
