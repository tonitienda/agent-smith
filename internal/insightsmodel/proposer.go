// Package insightsmodel is the provider-backed implementation of the AS-109
// insights model-assisted layer (PRD §7.14, Appendix C.3). It turns the measured
// insights.Report into richer, model-authored suggestions by asking a cheap-tier
// model to read the measured dashboard and propose additional, grounded advice.
//
// It lives outside internal/insights on purpose: insights points strictly inward
// (cost, projection, render, schema), so the part that reaches a provider and the
// routing policy lives here and is wired in by the composition root (cmd/smith).
// The package implements insights.Proposer, so the insights-writer calls it
// through that seam without importing provider or routing.
//
// Grounding discipline (§9, non-negotiable): the prompt requires every suggestion
// to cite the measured evidence (the #<seq> anchors already in the dashboard), and
// the insights-writer independently drops any suggestion that fails to — so a
// model that ignores the instruction cannot leak an ungrounded suggestion.
package insightsmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/insights"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/schema"
)

// maxSuggestions caps how many model-authored suggestions one pass returns, so the
// retro stays a short, actionable list rather than a wall of prose.
const maxSuggestions = 5

// maxOutputTokens bounds the model's reply. Keeping it small is the first half of
// the budget posture (the cheap tier is the other half): a bounded reply on a
// cheap model is a fraction of a cent, well under any sane per-session cap.
const maxOutputTokens = 512

// Proposer is the cheap-tier insights model layer. It resolves the cheap routing
// tier for the session's provider and asks that model for grounded suggestions.
// The per-session budget cap is enforced upstream by the sub-agent Runner against
// the writer's declared BudgetUSD; this proposer keeps its own spend tiny by
// pairing the cheap tier with a bounded reply (maxOutputTokens).
type Proposer struct {
	p         provider.Provider
	router    routing.Policy
	baseModel string
	table     *cost.Table
}

// New builds a Proposer over the session's provider and routing policy. baseModel
// is the session's active model (the routing fallback); table prices the call so
// the writer can charge it against the per-session budget.
func New(p provider.Provider, router routing.Policy, baseModel string, table *cost.Table) *Proposer {
	return &Proposer{p: p, router: router, baseModel: baseModel, table: table}
}

// compile-time check that *Proposer satisfies the seam the insights-writer calls.
var _ insights.Proposer = (*Proposer)(nil)

// Propose asks the cheap-tier model for grounded suggestions over the measured
// report and returns them with the dollars the call spent. It returns no error for
// an empty/garbled model reply — it simply yields no suggestions, leaving the
// measured dashboard untouched — and surfaces an error only when the turn itself
// fails to run.
func (m *Proposer) Propose(ctx context.Context, r insights.Report) ([]insights.Suggestion, float64, error) {
	model := m.router.Resolve(routing.Cheap, m.p.Name(), m.baseModel)

	req := provider.Request{
		Model: model,
		Context: []schema.Block{
			systemBlock(systemPrompt),
			userBlock(insights.Render(r)),
		},
		Params: provider.SamplingParams{MaxTokens: maxOutputTokens},
		Cache:  provider.CacheHints{Disabled: true}, // a one-shot prefix that will never recur
	}

	stream, err := m.p.Stream(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("insights model pass: %w", err)
	}
	events, err := provider.Collect(stream)
	if err != nil {
		return nil, 0, fmt.Errorf("insights model pass: %w", err)
	}

	var text strings.Builder
	var usage *schema.Tokens
	for _, e := range events {
		switch e.Type {
		case provider.EventTextDelta:
			text.WriteString(e.TextDelta)
		case provider.EventUsage:
			if e.Usage != nil {
				usage = e.Usage
			}
		}
	}
	return parse(text.String()), m.price(model, usage), nil
}

// price values the model call's usage against the configured table, so the spend
// is charged to the writer's per-session budget. With no table or an unpriced
// model it reports 0 (tokens-only), the same lower-bound posture cost.Summarize
// takes elsewhere.
func (m *Proposer) price(model string, usage *schema.Tokens) float64 {
	if usage == nil || m.table == nil {
		return 0
	}
	block := schema.Block{
		Kind:     eventlog.KindUsage,
		Tokens:   usage,
		Provider: &schema.Provider{Model: model},
	}
	return cost.Summarize([]schema.Block{block}, m.table).TotalUSD
}

// systemPrompt instructs the cheap model to act as the insights model layer: read
// the measured dashboard and propose additional, grounded suggestions, each citing
// a measured #<seq> anchor, as a strict JSON array.
const systemPrompt = `You are Agent Smith's insights model layer. You are given a measured session retrospective (token costs, repeated commands, oversized tool outputs, error loops, context health, and a goal if one was set).

Propose up to ` + "`5`" + ` additional, specific, actionable suggestions to improve future sessions. Each suggestion MUST cite the measured evidence it is grounded in using the #<seq> anchors that already appear in the dashboard — never invent numbers, never give generic advice. If the dashboard shows nothing worth acting on, return an empty array.

Reply with ONLY a JSON array, no prose, of objects with two string fields:
[{"summary": "<short actionable suggestion>", "evidence": "<the measured signal + #<seq> anchor it is grounded in>"}]`

// suggestionJSON is the wire shape the model replies in.
type suggestionJSON struct {
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
}

// parse extracts the suggestion array from the model's reply, tolerating prose or
// code fences around it by slicing to the outermost array. A reply that does not
// contain a usable array yields no suggestions (never an error): the measured
// dashboard stands on its own.
func parse(reply string) []insights.Suggestion {
	start := strings.IndexByte(reply, '[')
	end := strings.LastIndexByte(reply, ']')
	if start < 0 || end <= start {
		return nil
	}
	var raw []suggestionJSON
	if err := json.Unmarshal([]byte(reply[start:end+1]), &raw); err != nil {
		return nil
	}
	out := make([]insights.Suggestion, 0, len(raw))
	for _, s := range raw {
		if strings.TrimSpace(s.Summary) == "" || strings.TrimSpace(s.Evidence) == "" {
			continue
		}
		out = append(out, insights.Suggestion{
			Summary:  strings.TrimSpace(s.Summary),
			Evidence: strings.TrimSpace(s.Evidence),
			Source:   insights.SourceModel,
		})
		if len(out) == maxSuggestions {
			break
		}
	}
	return out
}

// systemBlock / userBlock build the two model-facing context blocks the pass
// sends: a system instruction and the rendered measured dashboard as user text.
func systemBlock(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: text}}
}

func userBlock(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: text}}
}
