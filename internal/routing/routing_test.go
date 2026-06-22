package routing

import "testing"

func TestDefaultResolvePreservesHardcodedCheapTier(t *testing.T) {
	p := Default()
	// The default policy must reproduce the pre-AS-042 hardcoded cheap models
	// exactly, so introducing the router changes no behavior (AS-038 AC4).
	if got := p.Resolve(Cheap, "anthropic", "active"); got != "claude-haiku-4-5" {
		t.Errorf("anthropic cheap = %q, want claude-haiku-4-5", got)
	}
	if got := p.Resolve(Cheap, "openai", "active"); got != "gpt-4o-mini" {
		t.Errorf("openai cheap = %q, want gpt-4o-mini", got)
	}
	// An unmapped vendor falls back to the caller's active model rather than
	// guessing an id the provider would reject.
	if got := p.Resolve(Cheap, "ollama", "llama3"); got != "llama3" {
		t.Errorf("unmapped vendor cheap = %q, want fallback llama3", got)
	}
	// standard/strong map nothing by default, so a feature on them stays on its
	// active model.
	if got := p.Resolve(Strong, "anthropic", "active"); got != "active" {
		t.Errorf("unmapped tier = %q, want fallback active", got)
	}
}

func TestFeatureTier(t *testing.T) {
	p := Default()
	if got := p.FeatureTier("compact", Cheap); got != Cheap {
		t.Errorf("no override = %q, want default cheap", got)
	}
	p.setFeature("compact", Standard)
	if got := p.FeatureTier("compact", Cheap); got != Standard {
		t.Errorf("override = %q, want standard", got)
	}
}

func TestTierOf(t *testing.T) {
	p := Default()
	if tier, ok := p.TierOf("claude-haiku-4-5"); !ok || tier != Cheap {
		t.Errorf("TierOf(haiku) = %q,%v, want cheap,true", tier, ok)
	}
	if _, ok := p.TierOf("claude-opus-4-8"); ok {
		t.Error("TierOf(opus) mapped to a tier, want unmapped (main model)")
	}
	if _, ok := p.TierOf(""); ok {
		t.Error("TierOf(empty) reported a tier, want false")
	}
}

func TestResolveEmptyMappingFallsBack(t *testing.T) {
	var p Policy
	p.set(Cheap, "anthropic", "") // empty model id must not shadow the fallback
	if got := p.Resolve(Cheap, "anthropic", "active"); got != "active" {
		t.Errorf("empty mapping = %q, want fallback active", got)
	}
}

func TestParseTier(t *testing.T) {
	for _, name := range []string{"cheap", "standard", "strong"} {
		if tier, ok := ParseTier(name); !ok || tier != Tier(name) {
			t.Errorf("ParseTier(%q) = %q,%v, want %s,true", name, tier, ok, name)
		}
	}
	if _, ok := ParseTier("turbo"); ok {
		t.Error("ParseTier(turbo) = true, want false for an unknown tier")
	}
}

func TestNextTier(t *testing.T) {
	if next, ok := NextTier(Cheap); !ok || next != Standard {
		t.Errorf("NextTier(cheap) = %q,%v, want standard,true", next, ok)
	}
	if next, ok := NextTier(Standard); !ok || next != Strong {
		t.Errorf("NextTier(standard) = %q,%v, want strong,true", next, ok)
	}
	if _, ok := NextTier(Strong); ok {
		t.Error("NextTier(strong) = true, want false (already strongest)")
	}
	if _, ok := NextTier("turbo"); ok {
		t.Error("NextTier(unknown) = true, want false")
	}
}

func TestWithFeatureTierDoesNotMutateBase(t *testing.T) {
	base := Default()
	over := base.WithFeatureTier("compact", Strong)
	if got := over.FeatureTier("compact", Cheap); got != Strong {
		t.Errorf("override FeatureTier = %q, want strong", got)
	}
	// The base policy the override was layered onto must be untouched, since the
	// config policy is shared across faces/sessions (AS-110 AC).
	if got := base.FeatureTier("compact", Cheap); got != Cheap {
		t.Errorf("base FeatureTier mutated = %q, want default cheap", got)
	}
}

func TestWithVendorModelDoesNotMutateBase(t *testing.T) {
	base := Default()
	over := base.WithVendorModel(Cheap, "anthropic", "claude-custom")
	if got := over.Resolve(Cheap, "anthropic", "active"); got != "claude-custom" {
		t.Errorf("override Resolve = %q, want claude-custom", got)
	}
	// base still maps the default cheap model — the override copied first.
	if got := base.Resolve(Cheap, "anthropic", "active"); got != "claude-haiku-4-5" {
		t.Errorf("base Resolve mutated = %q, want default claude-haiku-4-5", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	base := Default()
	c := base.Clone()
	c.set(Cheap, "anthropic", "mutated")
	c.setFeature("compact", Strong)
	if got := base.Resolve(Cheap, "anthropic", "active"); got != "claude-haiku-4-5" {
		t.Errorf("clone shares tier map: base = %q, want claude-haiku-4-5", got)
	}
	if got := base.FeatureTier("compact", Cheap); got != Cheap {
		t.Errorf("clone shares feature map: base = %q, want cheap", got)
	}
}
