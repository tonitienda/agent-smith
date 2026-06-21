package routing

import "fmt"

// configReader is the slice of the layered config this view reads; *config.Config
// satisfies it via Decode. Kept as a tiny consumer-side interface so this package
// owns the `routing` paths and their parsing without importing internal/config
// (AS-093: typed config views over the layered substrate).
type configReader interface {
	Decode(path string, v any) (bool, error)
}

// ConfigFrom reads the `routing` section over the built-in Default policy: config
// tiers and vendors add to or override the defaults, an unspecified tier keeps
// its default mapping, and per-feature overrides are keyed by feature name. A
// missing section yields Default unchanged. Malformed entries degrade with a
// warning rather than failing the session (PRD D2 tolerate-but-warn) — a routing
// typo must not block a run that can proceed on the default policy. The dotted
// path lives here, not in the composition root.
//
// Shape:
//
//	routing:
//	  tiers:
//	    cheap:    { anthropic: claude-haiku-4-5, openai: gpt-4o-mini }
//	    standard: { anthropic: claude-sonnet-4-6 }
//	    strong:   { anthropic: claude-opus-4-8 }
//	  features:
//	    compact: cheap
func ConfigFrom(c configReader) (Policy, []string) {
	var raw struct {
		Tiers    map[string]map[string]string `json:"tiers"`
		Features map[string]string            `json:"features"`
	}
	if _, err := c.Decode("routing", &raw); err != nil {
		return Default(), []string{fmt.Sprintf("ignoring routing config: %v", err)}
	}

	p := Default()
	var warns []string
	for name, vendors := range raw.Tiers {
		t := Tier(name)
		if !validTier(t) {
			warns = append(warns, fmt.Sprintf("routing.tiers.%s is not a known tier (cheap|standard|strong); ignoring", name))
			continue
		}
		for vendor, model := range vendors {
			if vendor == "" || model == "" {
				continue
			}
			p.set(t, vendor, model)
		}
	}
	for feat, name := range raw.Features {
		t := Tier(name)
		if !validTier(t) {
			warns = append(warns, fmt.Sprintf("routing.features.%s = %q is not a known tier (cheap|standard|strong); ignoring", feat, name))
			continue
		}
		p.setFeature(feat, t)
	}
	return p, warns
}
