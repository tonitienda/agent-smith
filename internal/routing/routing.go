// Package routing implements the model routing/tiering policy (AS-042): a small
// tier abstraction (cheap | standard | strong) mapped to concrete provider
// models in config, plus per-feature overrides. Tier-declaring features —
// /compact summarization (AS-038), system sub-agents (AS-044), user sub-agent
// defaults (AS-046) — resolve their concrete model through a Policy instead of
// hardcoding model ids, so retuning the cost/quality of mechanical subtasks is a
// config change, not a code change. PRD §7.15; "The Keymaker" in the §5 theme.
package routing

// Tier names a model capability/cost band. V1 routing applies only to explicitly
// tier-declared work (compaction, analyzers, sub-agents) — it never auto-downgrades
// the main interactive loop (AS-042 scope decision).
type Tier string

const (
	// Cheap is the fast/low-cost band for mechanical subtasks (search, summarize,
	// classify) — the band /compact and opt-in system sub-agents default to.
	Cheap Tier = "cheap"
	// Standard is the everyday reasoning band.
	Standard Tier = "standard"
	// Strong is the strongest band, for hard reasoning a feature opts into.
	Strong Tier = "strong"
)

// allTiers lists the tiers in a fixed order for rendering and reverse lookup.
// The order is also the escalation ladder (cheap → standard → strong) NextTier
// walks.
var allTiers = []Tier{Cheap, Standard, Strong}

// validTier reports whether name is a known tier.
func validTier(t Tier) bool {
	switch t {
	case Cheap, Standard, Strong:
		return true
	default:
		return false
	}
}

// ParseTier validates and normalizes a user-supplied tier name (e.g. from
// `/route <feature> <tier>`), reporting whether it names a known tier. It is the
// single parse point so the command surfaces and config share one notion of a
// valid tier.
func ParseTier(name string) (Tier, bool) {
	t := Tier(name)
	return t, validTier(t)
}

// NextTier returns the next stronger tier on the cheap → standard → strong
// ladder, or (_, false) when t is already the strongest (or unknown). It is the
// escalation step Escalate walks.
func NextTier(t Tier) (Tier, bool) {
	for i, cur := range allTiers {
		if cur == t && i+1 < len(allTiers) {
			return allTiers[i+1], true
		}
	}
	return "", false
}

// Policy maps each tier to a concrete model id per provider vendor, plus optional
// per-feature tier overrides keyed by feature/sub-agent name. It carries no
// mutating behavior once built (ConfigFrom and Default produce it), so the same
// value is safe to share across a session's engine rebuilds.
type Policy struct {
	tiers    map[Tier]map[string]string // tier -> vendor -> model id
	features map[string]Tier            // feature/sub-agent name -> tier override
}

// Default is the built-in policy used when config carries no `routing` section.
// It reproduces the previously hardcoded cheap-tier choice (AS-038 AC4) exactly —
// the active vendor's cheapest family — so introducing the router changes no
// behavior until config opts in. standard/strong map nothing, so a feature on
// those tiers stays on its caller's active model.
func Default() Policy {
	return Policy{
		tiers: map[Tier]map[string]string{
			Cheap: {"anthropic": "claude-haiku-4-5", "openai": "gpt-4o-mini"},
		},
	}
}

// Resolve returns the concrete model id for tier on vendor, or fallback when the
// policy maps no model there. An unmapped vendor/tier keeps the caller on its
// active model rather than guessing an id the provider would reject — the AS-038
// cheap-tier rule, now policy-driven.
func (p Policy) Resolve(tier Tier, vendor, fallback string) string {
	if m, ok := p.tiers[tier][vendor]; ok && m != "" {
		return m
	}
	return fallback
}

// FeatureTier returns the tier a feature/sub-agent runs on: its configured
// override when present, else def. It lets config remap, say, /compact off the
// cheap tier without touching the feature's code.
func (p Policy) FeatureTier(feature string, def Tier) Tier {
	if t, ok := p.features[feature]; ok {
		return t
	}
	return def
}

// TierOf reports which tier serves model, for the /route inspector to label
// recent calls. Model ids are vendor-unique in practice (claude-* vs gpt-*), so
// it searches across vendors and returns the first matching tier in cheap →
// standard → strong order. A model no tier maps (typically the main interactive
// model) returns ("", false).
func (p Policy) TierOf(model string) (Tier, bool) {
	if model == "" {
		return "", false
	}
	for _, t := range allTiers {
		for _, m := range p.tiers[t] {
			if m == model {
				return t, true
			}
		}
	}
	return "", false
}

// Clone returns a deep copy of the policy: the nested tier→vendor→model maps and
// the feature override map are duplicated, not shared. Policy values copy by
// reference (the maps are reference types), so a per-session override layer must
// Clone before mutating or it would silently rewrite the durable config-owned
// policy every face shares (AS-110; Gemini review on #216).
func (p Policy) Clone() Policy {
	c := Policy{}
	if p.tiers != nil {
		c.tiers = make(map[Tier]map[string]string, len(p.tiers))
		for t, vendors := range p.tiers {
			cv := make(map[string]string, len(vendors))
			for v, m := range vendors {
				cv[v] = m
			}
			c.tiers[t] = cv
		}
	}
	if p.features != nil {
		c.features = make(map[string]Tier, len(p.features))
		for f, t := range p.features {
			c.features[f] = t
		}
	}
	return c
}

// WithFeatureTier returns a copy of the policy with feature pinned to tier — the
// per-session `/route <feature> <tier>` override path (AS-110). It Clones first,
// so the durable config policy the receiver came from is never mutated; layering
// a second override over the result accumulates because each call copies the
// previous overrides.
func (p Policy) WithFeatureTier(feature string, tier Tier) Policy {
	c := p.Clone()
	c.setFeature(feature, tier)
	return c
}

// WithVendorModel returns a copy of the policy with tier→vendor mapped to model —
// the per-session `/route <tier> <vendor> <model>` override path (AS-110). Like
// WithFeatureTier it Clones first so the shared config policy stays untouched.
func (p Policy) WithVendorModel(tier Tier, vendor, model string) Policy {
	c := p.Clone()
	c.set(tier, vendor, model)
	return c
}

// set records a tier→vendor→model mapping, lazily allocating the nested maps. It
// is used by ConfigFrom over a fresh Default policy it owns.
func (p *Policy) set(t Tier, vendor, model string) {
	if p.tiers == nil {
		p.tiers = map[Tier]map[string]string{}
	}
	if p.tiers[t] == nil {
		p.tiers[t] = map[string]string{}
	}
	p.tiers[t][vendor] = model
}

// setFeature records a feature→tier override.
func (p *Policy) setFeature(feature string, t Tier) {
	if p.features == nil {
		p.features = map[string]Tier{}
	}
	p.features[feature] = t
}
