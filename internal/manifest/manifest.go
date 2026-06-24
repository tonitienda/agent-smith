// Package manifest builds and persists a run manifest — the small, replayable
// header that sits next to a session's append-only event log (AS-055, PRD §7.23
// OSS half). The log (D3) already *is* the reproducible record; the manifest is
// the derived index over it that answers, without replaying the whole log, "which
// models served this run, which tools ran, what did it cost, against which
// config". Every field is computed from the log plus the run's metadata, so the
// manifest is a cache, never a second source of truth: it can be rebuilt from the
// log at any time (and is, on `smith replay`).
//
// Secret hygiene is structural: the config snapshot is sanitized here (Sanitize),
// the single place that decides what config a manifest may carry, so keys and
// other secrets never reach the manifest even if they somehow appear in the
// resolved config. The token/cost figures come from internal/cost, which reads
// the same redacted log (AS-115), so the manifest inherits capture-time redaction.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/schema"
)

// File is the manifest's name within a session directory, a sibling of the event
// log and metadata file.
const File = "manifest.json"

// schemaVersion is bumped only additively (PRD D2); a reader tolerates unknown
// newer fields and a missing manifest alike.
const schemaVersion = 1

// Manifest is the per-session run record (AS-055). It is JSON-marshaled to
// manifest.json next to the event log. All fields are derived from the log or the
// run metadata, so the manifest never holds state the log cannot reconstruct.
type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	ProjectPath   string    `json:"project_path,omitempty"`
	Parent        string    `json:"parent,omitempty"` // delegating session (AS-046), if any
	CreatedAt     time.Time `json:"created_at"`
	GeneratedAt   time.Time `json:"generated_at"` // when this manifest was last (re)built
	BinaryVersion string    `json:"binary_version,omitempty"`

	Models     []string `json:"models,omitempty"` // distinct models that served a turn
	Tools      []string `json:"tools,omitempty"`  // distinct tool names invoked
	EventCount int      `json:"event_count"`
	Turns      int      `json:"turns"` // priced/usage turns on the log

	Totals Totals `json:"totals"`

	// Config is a sanitized snapshot of the effective config the run resolved
	// (Sanitize strips secret-looking keys). It answers "which config" without
	// ever carrying a credential.
	Config map[string]any `json:"config,omitempty"`
}

// Totals is the rolled-up token and dollar accounting from internal/cost, flattened
// into the manifest so a reader needs no pricing table to see the headline numbers.
type Totals struct {
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Priced           bool    `json:"priced"` // false when any turn's model lacked a price (CostUSD is a lower bound)
	Currency         string  `json:"currency,omitempty"`
}

// Input is everything Build needs that is not derivable from the log alone: the
// run's identity and metadata, the priced cost summary, and the raw effective
// config (sanitized inside Build). Events is the full event log.
type Input struct {
	SessionID     string
	ProjectPath   string
	Parent        string
	BinaryVersion string
	CreatedAt     time.Time
	GeneratedAt   time.Time
	Events        []schema.Block
	Cost          cost.Summary
	Config        map[string]any // raw effective config (dotted path -> value); sanitized by Build
}

// Build derives a Manifest from a run's log and metadata. It is pure (no I/O) so
// it is trivially testable and can be re-run to refresh a stale manifest.
func Build(in Input) Manifest {
	models, tools := scan(in.Events)
	return Manifest{
		SchemaVersion: schemaVersion,
		SessionID:     in.SessionID,
		ProjectPath:   in.ProjectPath,
		Parent:        in.Parent,
		CreatedAt:     in.CreatedAt,
		GeneratedAt:   in.GeneratedAt,
		BinaryVersion: in.BinaryVersion,
		Models:        models,
		Tools:         tools,
		EventCount:    len(in.Events),
		Turns:         len(in.Cost.Turns),
		Totals: Totals{
			InputTokens:      in.Cost.Total.Input,
			OutputTokens:     in.Cost.Total.Output,
			CacheReadTokens:  in.Cost.Total.CacheRead,
			CacheWriteTokens: in.Cost.Total.CacheWrite,
			TotalTokens:      in.Cost.Total.Total(),
			CostUSD:          in.Cost.TotalUSD,
			Priced:           in.Cost.AllPriced,
			Currency:         in.Cost.Currency,
		},
		Config: Sanitize(in.Config),
	}
}

// scan walks the log once, collecting the distinct models that served a turn and
// the distinct tool names invoked, each sorted for a stable manifest.
func scan(events []schema.Block) (models, tools []string) {
	modelSeen, toolSeen := map[string]bool{}, map[string]bool{}
	for _, b := range events {
		if b.Provider != nil && b.Provider.Model != "" && !modelSeen[b.Provider.Model] {
			modelSeen[b.Provider.Model] = true
			models = append(models, b.Provider.Model)
		}
		if b.Kind == schema.KindToolCall && b.ToolCall != nil && b.ToolCall.Name != "" && !toolSeen[b.ToolCall.Name] {
			toolSeen[b.ToolCall.Name] = true
			tools = append(tools, b.ToolCall.Name)
		}
	}
	sort.Strings(models)
	sort.Strings(tools)
	return models, tools
}

// secretKeyHints are the substrings that mark a config path as carrying a secret.
// Matching is case-insensitive on the dotted path so `provider.api_key`,
// `mcp.servers.x.token`, and `auth.credential` are all dropped from the snapshot.
// This is deliberately conservative — a false positive only omits a value from a
// diagnostic snapshot, while a false negative would leak a secret (PRD D0/D9).
var secretKeyHints = []string{"key", "token", "secret", "password", "passwd", "credential", "auth", "bearer"}

// Sanitize returns a copy of cfg with every entry whose dotted path looks like a
// secret removed. It is the single chokepoint for what config a manifest may
// carry, so the canary-secret guarantee (AS-055) is one testable function. A nil
// or empty map yields nil so the field stays omitempty.
func Sanitize(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if isSecretKey(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSecretKey(path string) bool {
	lower := strings.ToLower(path)
	for _, hint := range secretKeyHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// Write atomically persists m as manifest.json in dir, mirroring the session
// metadata write (temp file + rename + best-effort dir sync) so a crash mid-write
// never leaves a truncated manifest.
func Write(dir string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: marshal: %w", err)
	}
	b = append(b, '\n')

	path := filepath.Join(dir, File)
	tmp, err := os.CreateTemp(dir, File+".*.tmp")
	if err != nil {
		return fmt.Errorf("manifest: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("manifest: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("manifest: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("manifest: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("manifest: commit: %w", err)
	}
	cleanup = false
	return syncDirBestEffort(dir)
}

// Read loads the manifest from dir. The boolean is false (with a nil error) when
// no manifest has been written yet — an older session or one that never ran
// through a manifest-writing face — so the caller can rebuild it from the log.
func Read(dir string) (Manifest, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, File))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("manifest: read: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, false, fmt.Errorf("manifest: parse: %w", err)
	}
	return m, true, nil
}

func syncDirBestEffort(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("manifest: open directory for sync: %w", err)
	}
	if err := d.Sync(); err != nil && !errors.Is(err, fs.ErrInvalid) {
		closeErr := d.Close()
		return errors.Join(fmt.Errorf("manifest: sync directory: %w", err), closeErr)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("manifest: close directory after sync: %w", err)
	}
	return nil
}
