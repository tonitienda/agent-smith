package routing

import (
	"encoding/json"
	"errors"
	"testing"
)

// fakeConfig is a tiny configReader that decodes a preset tree by JSON
// round-trip, mirroring how *config.Config.Decode reads a subtree.
type fakeConfig struct {
	tree map[string]any
	err  error
}

func (f fakeConfig) Decode(path string, v any) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	sub, ok := f.tree[path]
	if !ok {
		return false, nil
	}
	data, err := json.Marshal(sub)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, err
	}
	return true, nil
}

func TestConfigFromMissingSectionIsDefault(t *testing.T) {
	p, warns := ConfigFrom(fakeConfig{tree: map[string]any{}})
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
	if got := p.Resolve(Cheap, "anthropic", "x"); got != "claude-haiku-4-5" {
		t.Errorf("default cheap lost: %q", got)
	}
}

func TestConfigFromOverridesAndAdds(t *testing.T) {
	p, warns := ConfigFrom(fakeConfig{tree: map[string]any{
		"routing": map[string]any{
			"tiers": map[string]any{
				"cheap":    map[string]any{"openai": "gpt-cheapo"},
				"standard": map[string]any{"anthropic": "claude-sonnet-4-6"},
				"strong":   map[string]any{"anthropic": "claude-opus-4-8"},
			},
			"features": map[string]any{"compact": "standard"},
		},
	}})
	if len(warns) != 0 {
		t.Fatalf("warns = %v, want none", warns)
	}
	// Overridden cheap vendor.
	if got := p.Resolve(Cheap, "openai", "x"); got != "gpt-cheapo" {
		t.Errorf("cheap openai = %q, want gpt-cheapo", got)
	}
	// Default cheap vendor not touched by the override survives.
	if got := p.Resolve(Cheap, "anthropic", "x"); got != "claude-haiku-4-5" {
		t.Errorf("cheap anthropic = %q, want default claude-haiku-4-5", got)
	}
	// Added tiers resolve.
	if got := p.Resolve(Standard, "anthropic", "x"); got != "claude-sonnet-4-6" {
		t.Errorf("standard = %q", got)
	}
	if got := p.Resolve(Strong, "anthropic", "x"); got != "claude-opus-4-8" {
		t.Errorf("strong = %q", got)
	}
	// Feature override applies.
	if got := p.FeatureTier("compact", Cheap); got != Standard {
		t.Errorf("compact tier = %q, want standard", got)
	}
}

func TestConfigFromInvalidTierWarns(t *testing.T) {
	p, warns := ConfigFrom(fakeConfig{tree: map[string]any{
		"routing": map[string]any{
			"tiers":    map[string]any{"turbo": map[string]any{"anthropic": "x"}},
			"features": map[string]any{"compact": "turbo"},
		},
	}})
	if len(warns) != 2 {
		t.Errorf("warns = %v, want 2 (bad tier + bad feature tier)", warns)
	}
	// The bad tier left no mapping and the default policy is intact.
	if got := p.Resolve(Cheap, "anthropic", "x"); got != "claude-haiku-4-5" {
		t.Errorf("default lost after bad config: %q", got)
	}
	if got := p.FeatureTier("compact", Cheap); got != Cheap {
		t.Errorf("bad feature override applied: %q", got)
	}
}

func TestConfigFromDecodeErrorDegrades(t *testing.T) {
	p, warns := ConfigFrom(fakeConfig{err: errors.New("boom")})
	if len(warns) != 1 {
		t.Errorf("warns = %v, want 1", warns)
	}
	if got := p.Resolve(Cheap, "anthropic", "x"); got != "claude-haiku-4-5" {
		t.Errorf("decode error did not degrade to default: %q", got)
	}
}
