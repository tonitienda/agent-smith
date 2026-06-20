package budget

import "fmt"

// configReader is the slice of the layered config the budget view reads;
// *config.Config satisfies it via Decode. Kept as a tiny consumer-side interface
// so this package owns the `budget` paths and their parsing without importing
// internal/config (AS-093: typed config views over the layered substrate).
type configReader interface {
	Decode(path string, v any) (bool, error)
}

// Config is the validated budget settings (AS-041/AS-086) read from the `budget`
// section: the default per-session ceiling, the warn fraction, and whether an
// unpriced model halts a budgeted run. Every field is optional; a missing
// section leaves the zero Config (enforcement disabled). The defaulting of the
// warn fraction itself stays in Guard, the one place that maps a spend total to
// a State, so this view only reads and validates what config carries.
type Config struct {
	// DefaultLimitUSD is the ceiling applied to new sessions that carry no
	// /budget override. Zero (or negative) disables enforcement.
	DefaultLimitUSD float64
	// WarnFraction is the fraction of the ceiling at which warnings begin. Zero
	// (or out of range) defers to Guard's package default.
	WarnFraction float64
	// HaltUnpriced (AS-086) decides whether a budgeted session stops, rather than
	// spending blind, when the active model has no pricing entry.
	HaltUnpriced bool
}

// ConfigFrom reads the `budget` section out of the layered config into a
// validated Config. A missing section yields the zero Config. A malformed
// section degrades to the zero Config with a warning rather than failing the
// session — the tolerate-but-warn rule (PRD D2): a budget typo must not block a
// run that could proceed unmetered. The dotted path lives here, not in the
// composition root.
func ConfigFrom(c configReader) (Config, []string) {
	var raw struct {
		LimitUSD     float64 `json:"limit_usd"`
		WarnFraction float64 `json:"warn_fraction"`
		HaltUnpriced bool    `json:"halt_unpriced"`
	}
	if _, err := c.Decode("budget", &raw); err != nil {
		return Config{}, []string{fmt.Sprintf("ignoring budget config: %v", err)}
	}
	var warns []string
	if raw.LimitUSD < 0 {
		warns = append(warns, fmt.Sprintf("budget.limit_usd is negative (%g); ignoring", raw.LimitUSD))
		raw.LimitUSD = 0
	}
	if raw.WarnFraction < 0 || raw.WarnFraction >= 1 {
		warns = append(warns, fmt.Sprintf("budget.warn_fraction %g is out of range (0,1); using the default", raw.WarnFraction))
		raw.WarnFraction = 0
	}
	return Config{
		DefaultLimitUSD: raw.LimitUSD,
		WarnFraction:    raw.WarnFraction,
		HaltUnpriced:    raw.HaltUnpriced,
	}, warns
}
