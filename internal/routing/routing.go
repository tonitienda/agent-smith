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
