package insights

import (
	"context"
	"strings"
)

// Proposer is the AS-109 model-assisted layer: given the measured Report, it
// returns additional, richer suggestions a cheap-tier model authored, plus the
// dollars the call spent. It is the seam the insights-writer calls when the model
// layer is enabled; the concrete, provider-backed implementation lives outside
// this package (cmd/smith wires internal/insightsmodel) so insights keeps pointing
// inward (cost, projection, render, schema only).
//
// §9 mitigation, non-negotiable: every suggestion a Proposer returns must cite the
// measured evidence it is grounded in. The writer enforces this — a suggestion
// whose Evidence carries no measured anchor is dropped, never shown — so a
// hallucinated suggestion cannot reach the user even if the model emits one.
type Proposer interface {
	// Propose returns model-authored suggestions grounded in r and the dollars
	// spent producing them. It must stay within the budget it was configured with;
	// an error means the layer is skipped this session (the measured dashboard is
	// unaffected).
	Propose(ctx context.Context, r Report) (suggestions []Suggestion, spentUSD float64, err error)
}

// Grounded returns the subset of suggestions that cite measured evidence (§9),
// each tagged SourceModel. It is the on-demand twin of the gate the writer applies
// at session end (AS-137): the `/insights describe` path merges only these into the
// rendered dashboard, so an ungrounded, model-authored suggestion never reaches the
// user even when the model ignores the grounding instruction.
func Grounded(suggestions []Suggestion) []Suggestion {
	out := make([]Suggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if !citesMeasuredEvidence(s) {
			continue
		}
		s.Source = SourceModel
		out = append(out, s)
	}
	return out
}

// citesMeasuredEvidence reports whether a suggestion's evidence carries a
// jump-to-transcript anchor ("#<seq>"). It is the §9 grounding gate the writer
// applies to every model-authored suggestion before recording it: prose with no
// measured anchor is vibes, and vibes never land.
func citesMeasuredEvidence(s Suggestion) bool {
	for i := strings.IndexByte(s.Evidence, '#'); i >= 0 && i+1 < len(s.Evidence); {
		if c := s.Evidence[i+1]; c >= '0' && c <= '9' {
			return true
		}
		next := strings.IndexByte(s.Evidence[i+1:], '#')
		if next < 0 {
			break
		}
		i += next + 1
	}
	return false
}
