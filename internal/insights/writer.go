package insights

import (
	"context"

	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// Name is the built-in insights-writer sub-agent's stable registry name (Appendix
// C.3 `insights_writer`).
const Name = "insights-writer"

// FindingKind tags every finding the writer emits, so /insights and the
// cross-session rollup (AS-050/AS-057) can recognize an insights suggestion.
const FindingKind = "insights"

// Writer is the insights-writer system sub-agent (AS-044/AS-107, PRD §7.19): a
// passive analyzer that runs once at session end over the whole session and
// reports the measured-signal suggestions as findings. It calls no model — the
// retrospective is computed from the log — so it is within budget when enabled
// and free when disabled, the same posture as the rediscovered-fact detector.
//
// It is stateless across the lifecycle (analysis happens in Teardown over the
// handed slice), so a single instance is reusable; Factory yields fresh ones
// anyway, per the framework's per-session instancing rule.
type Writer struct {
	// proposer, modelTier and budgetUSD are set only when the AS-109 model-assisted
	// layer is configured (subagents.insights_writer.model). When proposer is nil
	// the writer is the original measured-first, zero-cost analyzer — the default.
	proposer  Proposer
	modelTier string
	budgetUSD float64
}

// New builds a measured-first Writer (no model layer): the original zero-cost,
// default-on retrospective analyzer.
func New() *Writer { return &Writer{} }

// NewWithModel builds a Writer whose teardown runs the AS-109 model-assisted layer
// after the measured pass: it calls proposer on the cheap tier (tier) within
// budgetUSD and appends the grounded, model-authored suggestions as findings. A
// nil proposer or an empty tier yields the measured-first writer (the model layer
// stays off), so installing it is safe even when the model layer is unconfigured.
func NewWithModel(proposer Proposer, tier string, budgetUSD float64) *Writer {
	if proposer == nil || tier == "" {
		return New()
	}
	return &Writer{proposer: proposer, modelTier: tier, budgetUSD: budgetUSD}
}

// Factory returns a subagent.Factory that builds measured-first Writers.
func Factory() subagent.Factory {
	return func() subagent.SubAgent { return New() }
}

// FactoryWithModel returns a subagent.Factory that builds model-layer Writers
// (AS-109). The composition root passes a provider-backed Proposer plus the
// configured cheap tier and per-session budget; a nil proposer falls back to the
// measured-first writer.
func FactoryWithModel(proposer Proposer, tier string, budgetUSD float64) subagent.Factory {
	return func() subagent.SubAgent { return NewWithModel(proposer, tier, budgetUSD) }
}

// Manifest declares the writer: a passive analyzer that tears down once at
// session end over the whole session, defaults on, proposes memory edits, and
// reads the transcript. Without the model layer it declares no model tier and a
// zero budget, so it costs nothing even when enabled; with the AS-109 model layer
// configured it declares the cheap tier and the per-session budget cap, which the
// Runner enforces before spending (the §7.19 budget AC).
func (w *Writer) Manifest() subagent.Manifest {
	return subagent.Manifest{
		Name:             Name,
		Kind:             subagent.KindAnalyzer,
		Schedule:         subagent.AtSessionEnd,
		Scope:            subagent.SessionScope,
		EnabledByDefault: true,
		ModelTier:        w.modelTier, // "" (measured-first) unless the model layer is configured
		BudgetUSD:        w.budgetUSD, // 0 unless the model layer is configured
		Emits:            []string{FindingKind},
		Permissions:      []subagent.Permission{subagent.ReadTranscript, subagent.ProposeEdit},
	}
}

// Init is a no-op: there is no per-scope state to set up.
func (w *Writer) Init(subagent.Scope) {}

// Observe is a no-op: the writer analyzes the slice handed to Teardown rather
// than accumulating per block, so it adds no per-block work to a turn.
func (w *Writer) Observe(schema.Block) {}

// Teardown analyzes the session's blocks and returns one finding per suggestion,
// carrying the propose-only memory edit when the suggestion is applicable. It
// prices nothing (a nil cost table), so it spends nothing and is never
// budget-capped — the dollar figures live in the /insights panel, which has the
// pricing table; the findings are the actionable suggestions.
func (w *Writer) Teardown(scope subagent.Scope, slice []schema.Block) subagent.Result {
	rep := Analyze(slice, nil, "")
	findings := make([]subagent.Finding, 0, len(rep.Suggestions))
	for _, s := range rep.Suggestions {
		findings = append(findings, toFinding(s))
	}

	// AS-109 model-assisted layer: when configured, the cheap-tier Proposer adds
	// richer, model-authored suggestions on top of the measured ones. Each must
	// cite measured evidence (§9) or it is dropped — a hallucinated suggestion
	// never lands. A Proposer error degrades to the measured findings rather than
	// failing the teardown; the dashboard already rendered for free.
	spent := 0.0
	if w.proposer != nil {
		proposed, cost, err := w.proposer.Propose(context.Background(), rep)
		if err == nil {
			spent = cost
			for _, s := range proposed {
				if !citesMeasuredEvidence(s) {
					continue
				}
				s.Source = SourceModel
				findings = append(findings, toFinding(s))
			}
		}
	}
	return subagent.Result{Findings: findings, SpentUSD: spent}
}

// toFinding maps a measured or model-authored suggestion to a recorded finding,
// carrying the propose-only memory edit when the suggestion is applicable.
func toFinding(s Suggestion) subagent.Finding {
	f := subagent.Finding{
		Kind:    FindingKind,
		Summary: s.Summary,
		Detail:  s.Evidence,
	}
	if s.Source == SourceModel {
		f.Summary += " (model)"
	}
	if s.Edit != nil {
		f.Proposal = &subagent.Edit{
			Target:      s.Edit.Target,
			Description: "+ " + s.Edit.Line,
		}
	}
	return f
}
