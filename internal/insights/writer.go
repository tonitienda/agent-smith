package insights

import (
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
type Writer struct{}

// New builds a Writer.
func New() *Writer { return &Writer{} }

// Factory returns a subagent.Factory that builds Writers.
func Factory() subagent.Factory {
	return func() subagent.SubAgent { return New() }
}

// Manifest declares the writer: a passive analyzer that tears down once at
// session end over the whole session, defaults on (it costs nothing — no model
// tier), proposes memory edits, and reads the transcript.
func (w *Writer) Manifest() subagent.Manifest {
	return subagent.Manifest{
		Name:             Name,
		Kind:             subagent.KindAnalyzer,
		Schedule:         subagent.AtSessionEnd,
		Scope:            subagent.SessionScope,
		EnabledByDefault: true,
		ModelTier:        "", // measured-first: no model use → zero cost even when enabled
		BudgetUSD:        0,
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
		f := subagent.Finding{
			Kind:    FindingKind,
			Summary: s.Summary,
			Detail:  s.Evidence,
		}
		if s.Edit != nil {
			f.Proposal = &subagent.Edit{
				Target:      s.Edit.Target,
				Description: "+ " + s.Edit.Line,
			}
		}
		findings = append(findings, f)
	}
	return subagent.Result{Findings: findings}
}
