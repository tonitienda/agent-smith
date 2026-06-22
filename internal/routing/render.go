package routing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/render"
)

// Call is one recent provider turn the /route inspector labels with the tier
// that served it (AS-042 AC: "which tier served each recent call").
type Call struct {
	Index int    // 1-based turn number in log order
	Model string // the model id the turn ran on
}

// unmapped marks a model no tier owns — the main interactive model, which V1
// routing never re-tiers.
const unmapped = "main"

// dash is shown where a tier maps no model on any vendor, or a model id is empty.
const dash = "—"

// Render formats the active policy for the /route inspector: the tier→model
// mapping per vendor, the per-feature overrides, (when recent is non-empty) the
// tier that served each recent call, and (when escalations is non-empty) the
// auto-escalations that occurred this session — the feature, the tiers it moved
// between, and the structured reason (AS-116). It is face-agnostic so the TUI and
// a headless face render the same view.
func Render(p Policy, recent []Call, escalations []Escalation) string {
	var b strings.Builder
	b.WriteString("Model routing policy (AS-042)\n\n")

	b.WriteString("Tiers\n")
	tw, row := render.Tab(&b, 0)
	for _, t := range allTiers {
		row("  %s\t%s\t\n", t, vendorList(p.tiers[t]))
	}
	_ = tw.Flush()

	b.WriteString("\nFeature overrides\n")
	if len(p.features) == 0 {
		b.WriteString("  (none — every feature uses its default tier)\n")
	} else {
		ftw, frow := render.Tab(&b, 0)
		for _, f := range sortedKeys(p.features) {
			frow("  %s\t%s\t\n", f, p.features[f])
		}
		_ = ftw.Flush()
	}

	if len(recent) > 0 {
		b.WriteString("\nRecent calls\n")
		rtw, rrow := render.Tab(&b, 0)
		for _, c := range recent {
			tier := unmapped
			if t, ok := p.TierOf(c.Model); ok {
				tier = string(t)
			}
			rrow("  #%d\t%s\t%s\t\n", c.Index, modelLabel(c.Model), tier)
		}
		_ = rtw.Flush()
	}

	if len(escalations) > 0 {
		b.WriteString("\nEscalations\n")
		etw, erow := render.Tab(&b, 0)
		for _, e := range escalations {
			erow("  %s\t%s → %s\t%s\t\n", e.Feature, e.From, e.To, e.Reason)
		}
		_ = etw.Flush()
	}

	return strings.TrimRight(b.String(), "\n")
}

// vendorList renders a tier's vendor→model mappings as a sorted, space-joined
// "vendor=model" list, or a note that the tier falls back to the active model.
func vendorList(vendors map[string]string) string {
	if len(vendors) == 0 {
		return "(falls back to the active model)"
	}
	keys := make([]string, 0, len(vendors))
	for v := range vendors {
		keys = append(keys, v)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, v := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", v, modelLabel(vendors[v])))
	}
	return strings.Join(parts, "  ")
}

// sortedKeys returns the feature names in stable order for deterministic output.
func sortedKeys(m map[string]Tier) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func modelLabel(m string) string {
	if m == "" {
		return dash
	}
	return m
}
