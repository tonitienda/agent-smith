package cost

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed data/pricing.json
var embeddedData embed.FS

// EnvPricingFile names an environment variable holding a path to a pricing JSON
// file. When set, its rates override the embedded table model-by-model (a model
// absent from the override falls back to the embedded rate), so a price change
// or a new model needs no recompile (AS-020).
const EnvPricingFile = "SMITH_PRICING"

// Rate is the list price of a model's usage, in currency units per million
// tokens. CacheRead/CacheWrite default to zero, which prices providers with no
// (or free) prompt caching correctly.
type Rate struct {
	// Model is an exact model ID (e.g. "claude-opus-4-8") or a prefix pattern
	// ending in "*" (e.g. "claude-opus-4-*"). On lookup an exact match wins; among
	// patterns the longest matching prefix wins, so a specific entry beats a broad
	// family default.
	Model             string  `json:"model"`
	Vendor            string  `json:"vendor,omitempty"`
	InputPerMTok      float64 `json:"input_per_mtok"`
	OutputPerMTok     float64 `json:"output_per_mtok"`
	CacheReadPerMTok  float64 `json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMTok float64 `json:"cache_write_per_mtok,omitempty"`

	// ContextWindow is the model's maximum context-window size in tokens. The
	// context meter (AS-025) uses it as the denominator for how full the window
	// is; zero means the window is unknown, so the meter shows the raw token
	// count without a percentage. The field is optional and additive (PRD D2).
	ContextWindow int `json:"context_window,omitempty"`

	// MaxOutputTokens is the model's maximum output (completion) length in tokens.
	// The pre-turn budget reservation (AS-086) prices it at the output rate as the
	// worst-case generation cost of the next turn; zero means it is unknown, so the
	// reservation cannot bound the turn and the guard falls back to AS-041's
	// boundary check. Optional and additive (PRD D2).
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
}

// wireTable is the on-disk pricing document.
type wireTable struct {
	Version  int    `json:"version"`
	Currency string `json:"currency"`
	Models   []Rate `json:"models"`
}

// Table is a model-pricing lookup. A Table may chain to a parent: its own rates
// are consulted first and the parent supplies the fallback, which is how a user
// override layers over the embedded defaults.
type Table struct {
	rates    []Rate
	currency string
	parent   *Table
}

// Currency reports the table's currency code (e.g. "USD"), falling back to the
// parent's and finally to "USD".
func (t *Table) Currency() string {
	switch {
	case t == nil:
		return "USD"
	case t.currency != "":
		return t.currency
	case t.parent != nil:
		return t.parent.Currency()
	default:
		return "USD"
	}
}

// Lookup returns the rate for model, consulting this table before its parent. ok
// is false when no entry matches, so callers can show tokens while marking the
// dollar figure unknown (AS-020: unknown model degrades gracefully). An empty
// model is unspecified and never priced — without this guard a bare "*" pattern
// in an override would match it (strings.HasPrefix("", "") is true).
func (t *Table) Lookup(model string) (Rate, bool) {
	if t == nil || model == "" {
		return Rate{}, false
	}
	if r, ok := t.lookupLocal(model); ok {
		return r, true
	}
	return t.parent.Lookup(model)
}

// Window returns the context-window size (in tokens) for model, resolved the
// same way as pricing: an exact model ID wins, otherwise the longest matching
// "prefix*" pattern. ok is false when no entry matches or the matched entry
// records no window, so the context meter (AS-025) can fall back to showing raw
// token counts without a percentage.
func (t *Table) Window(model string) (int, bool) {
	r, ok := t.Lookup(model)
	if !ok || r.ContextWindow <= 0 {
		return 0, false
	}
	return r.ContextWindow, true
}

// lookupLocal matches model against this table's own rates only: an exact model
// ID wins outright, otherwise the longest matching "prefix*" pattern wins.
func (t *Table) lookupLocal(model string) (Rate, bool) {
	var best Rate
	bestLen := -1
	for _, r := range t.rates {
		if r.Model == model {
			return r, true
		}
		if prefix, ok := strings.CutSuffix(r.Model, "*"); ok &&
			strings.HasPrefix(model, prefix) && len(prefix) > bestLen {
			best, bestLen = r, len(prefix)
		}
	}
	return best, bestLen >= 0
}

// Models returns the pricing entries the table knows, this table's own rates
// layered over the parent's so a child entry for the same Model wins. The order
// is unspecified — callers that display the list should sort it. It lets a face
// enumerate the configured model families (e.g. the /model command, AS-023)
// without reaching into the table's internals.
func (t *Table) Models() []Rate {
	if t == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []Rate
	for tbl := t; tbl != nil; tbl = tbl.parent {
		for _, r := range tbl.rates {
			if !seen[r.Model] {
				seen[r.Model] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// Embedded returns the built-in pricing table shipped with the binary. It panics
// on a malformed embed because that is a build-time defect in our own data, not
// a runtime condition a caller can handle.
func Embedded() *Table {
	t, err := parse(mustReadEmbedded())
	if err != nil {
		panic("cost: embedded pricing table is invalid: " + err.Error())
	}
	return t
}

// LoadFile parses a pricing table from path.
func LoadFile(path string) (*Table, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path comes from the operator's own env var
	if err != nil {
		return nil, fmt.Errorf("cost: read pricing file: %w", err)
	}
	t, err := parse(b)
	if err != nil {
		return nil, fmt.Errorf("cost: parse pricing file %s: %w", path, err)
	}
	return t, nil
}

// ParseTable parses a pricing table from JSON bytes (the same `{version,
// currency, models}` document the embedded table and override files use). It
// lets a caller build a table from a pricing section read out of the unified
// config (AS-071) without reaching into the cost package's internals.
func ParseTable(b []byte) (*Table, error) { return parse(b) }

// Default returns the session pricing table from the embedded defaults plus the
// $SMITH_PRICING escape hatch (DefaultWith with no config section).
func Default() (*Table, error) { return DefaultWith(nil) }

// DefaultWith returns the pricing table to use for a session, layered low-to-high:
// the embedded defaults, then the unified config's `pricing` section (section,
// the marshaled JSON of that subtree — empty when unset), then the override file
// named by $SMITH_PRICING. Each higher layer overrides matching models and falls
// back to the layer below for the rest, so a config or env override needs no
// recompile. A set-but-unreadable/invalid section or file is a reported error so
// a typo never silently misprices a session.
func DefaultWith(section []byte) (*Table, error) {
	tbl := Embedded()
	if len(strings.TrimSpace(string(section))) > 0 {
		override, err := ParseTable(section)
		if err != nil {
			return nil, fmt.Errorf("cost: parse pricing config section: %w", err)
		}
		override.parent = tbl
		tbl = override
	}
	if path := strings.TrimSpace(os.Getenv(EnvPricingFile)); path != "" {
		override, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		override.parent = tbl
		tbl = override
	}
	return tbl, nil
}

// supportedVersion is the only pricing-schema version this build understands.
// The schema is additive-only (PRD D2), so compatible changes keep version 1; a
// bump would signal a breaking change, which is exactly why parse rejects any
// other version rather than risk silently mispricing an incompatible file.
const supportedVersion = 1

func parse(b []byte) (*Table, error) {
	var w wireTable
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, err
	}
	if w.Version != supportedVersion {
		return nil, fmt.Errorf("unsupported pricing table version %d (supported: %d)", w.Version, supportedVersion)
	}
	return &Table{rates: w.Models, currency: w.Currency}, nil
}

func mustReadEmbedded() []byte {
	b, err := embeddedData.ReadFile("data/pricing.json")
	if err != nil {
		panic("cost: embedded pricing table missing: " + err.Error())
	}
	return b
}
