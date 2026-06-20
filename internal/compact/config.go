package compact

import "fmt"

// DefaultAutoThreshold is the context-window fraction auto-compaction (AS-085)
// triggers at when the `compact.auto_threshold` setting is unset or out of the
// open interval (0,1). It is deliberately high: auto-compaction is the blunt
// last-resort guard against a context-window-exceeded stop, behind /clean and
// /tidy, so it fires only when the window is nearly full.
const DefaultAutoThreshold = 0.85

// configReader is the slice of the layered config the compact view reads;
// *config.Config satisfies it via Decode. Kept as a tiny consumer-side interface
// so this package owns the `compact` paths and their parsing without importing
// internal/config (AS-093: typed config views over the layered substrate).
type configReader interface {
	Decode(path string, v any) (bool, error)
}

// Config is the validated auto-compaction settings (AS-085) read from the
// `compact` section: whether the last-resort guard is on, and the window
// fraction it triggers at. Auto-compaction is off by default — the product
// prefers /clean and /tidy.
type Config struct {
	// Auto enables the blunt last-resort auto-compaction guard.
	Auto bool
	// AutoThreshold is the context-window fraction the guard triggers at, always
	// normalized to (0,1); DefaultAutoThreshold stands in for an unset or
	// out-of-range value.
	AutoThreshold float64
}

// ConfigFrom reads the `compact` section out of the layered config into a
// validated Config. A missing or malformed section yields the defaults (guard
// off, DefaultAutoThreshold) — the tolerate-but-warn rule (PRD D2): a typo must
// not block a session, and a stray threshold of 0 or 1 falls back rather than
// disabling the guard outright. The dotted paths and the threshold defaulting
// live here, with the feature, not in the composition root.
func ConfigFrom(c configReader) (Config, []string) {
	cfg := Config{AutoThreshold: DefaultAutoThreshold}
	var raw struct {
		Auto bool `json:"auto"`
		// Pointer so an unset threshold (silently defaults) is told apart from an
		// explicit out-of-range one (worth a warning, per D2's tolerate-but-warn).
		AutoThreshold *float64 `json:"auto_threshold"`
	}
	if _, err := c.Decode("compact", &raw); err != nil {
		return cfg, []string{fmt.Sprintf("ignoring compact config: %v", err)}
	}
	cfg.Auto = raw.Auto
	if raw.AutoThreshold == nil {
		return cfg, nil
	}
	if v := *raw.AutoThreshold; v > 0 && v < 1 {
		cfg.AutoThreshold = v
		return cfg, nil
	}
	return cfg, []string{fmt.Sprintf("compact.auto_threshold %g is out of range (0,1); using the default %g", *raw.AutoThreshold, DefaultAutoThreshold)}
}
