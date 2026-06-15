// Package cost is Agent Smith's token & cost accounting engine (AS-020, PRD
// §7.10, D5/D6). It turns the usage events the loop records on the append-only
// log (eventlog.KindUsage) into per-turn and per-session token + dollar
// accounting, broken down by input / output / cache, plus the cache savings the
// caching layer (AS-011) bought. Accounting is derived from the log, never a
// side table, so it survives save/resume and reconciles exactly with what the
// providers reported.
//
// Pricing is data, not code: rates ship as an embedded table (pricing.json),
// overridable per session by a file so a price change needs no release, and an
// unknown model degrades gracefully — tokens are still shown, the dollar figure
// is marked unknown. The engine is the data layer for the context meter
// (AS-025), the /context composition view (AS-026), and the D5 guardrail
// benchmarks, so accuracy matters more than presentation.
package cost

import (
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// Tokens is a flattened, nil-free view of schema.Tokens for display and
// aggregation: an unreported count reads as zero here (the "missing vs zero"
// distinction is preserved on the log itself, not in an accounting total).
type Tokens struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Reasoning  int
}

// Total is the sum of the billable token counts (input + output + cache read +
// cache write). Reasoning tokens are part of Output for every surveyed provider,
// so they are not added again.
func (t Tokens) Total() int { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func (t Tokens) add(o Tokens) Tokens {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
	t.Reasoning += o.Reasoning
	return t
}

// flatten projects a logged schema.Tokens onto the nil-free view.
func flatten(t *schema.Tokens) Tokens {
	if t == nil {
		return Tokens{}
	}
	return Tokens{
		Input:      deref(t.Input),
		Output:     deref(t.Output),
		CacheRead:  deref(t.CacheRead),
		CacheWrite: deref(t.CacheWrite),
		Reasoning:  deref(t.Reasoning),
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// TurnCost is one provider turn's priced usage — one eventlog.KindUsage record
// on the log. When Priced is false the model had no pricing entry: Tokens are
// still accurate, but the dollar figures are zero and must be shown as unknown.
type TurnCost struct {
	Index      int    // 1-based turn number in log order
	Model      string // the model that served the turn
	StopReason string
	Tokens     Tokens
	Priced     bool

	InputUSD      float64
	OutputUSD     float64
	CacheReadUSD  float64
	CacheWriteUSD float64
	TotalUSD      float64

	// CacheSavingsUSD is what reading CacheRead tokens from cache saved versus
	// paying the full input rate for them. Zero when unpriced or uncached.
	CacheSavingsUSD float64
}

// ContextTokens estimates the context-window occupancy at this turn: the tokens
// the provider counted in the prompt (fresh input plus cache reads and writes)
// plus the output it generated, all of which the next request carries. It is the
// figure the context meter (AS-025) shows from the most recent turn until
// per-block estimates (AS-063) replace it with a composed window size.
func (t TurnCost) ContextTokens() int {
	return t.Tokens.Input + t.Tokens.CacheRead + t.Tokens.CacheWrite + t.Tokens.Output
}

// Summary is the session-wide accounting: every priced turn plus the rolled-up
// totals the /cost command renders.
type Summary struct {
	Turns    []TurnCost
	Total    Tokens
	TotalUSD float64

	// CacheReadTokens and CacheSavingsUSD report the caching payoff across the
	// session in tokens and dollars (AS-020 acceptance: cache savings shown in
	// both units).
	CacheReadTokens int
	CacheSavingsUSD float64

	// AllPriced is false when at least one turn's model had no pricing entry, so
	// the session dollar total is a lower bound. Currency is the table's code.
	AllPriced bool
	Currency  string
}

// Latest returns the most recent turn (the last usage event in log order) and
// whether one exists — priced or not, since its token counts are exact even when
// the model has no pricing entry. The context meter (AS-025) uses it to size the
// live window from the prompt the provider last counted, with no extra model
// call.
func (s Summary) Latest() (TurnCost, bool) {
	if len(s.Turns) == 0 {
		return TurnCost{}, false
	}
	return s.Turns[len(s.Turns)-1], true
}

// Summarize prices the usage events in events against table and rolls them into
// a Summary. It reads only eventlog.KindUsage records, so it works on a full
// session log, a projection's blocks, or any block slice. A nil table prices
// nothing (every turn is Priced=false) but still reports exact token counts.
func Summarize(events []schema.Block, table *Table) Summary {
	s := Summary{AllPriced: true, Currency: table.Currency()}
	for _, b := range events {
		if b.Kind != eventlog.KindUsage {
			continue
		}
		tc := priceTurn(len(s.Turns)+1, b, table)
		s.Turns = append(s.Turns, tc)

		s.Total = s.Total.add(tc.Tokens)
		s.TotalUSD += tc.TotalUSD
		s.CacheReadTokens += tc.Tokens.CacheRead
		s.CacheSavingsUSD += tc.CacheSavingsUSD
		if !tc.Priced {
			s.AllPriced = false
		}
	}
	return s
}

// priceTurn builds the priced record for one usage block.
func priceTurn(index int, b schema.Block, table *Table) TurnCost {
	tc := TurnCost{
		Index:      index,
		StopReason: b.StopReason,
		Tokens:     flatten(b.Tokens),
	}
	if b.Provider != nil {
		tc.Model = b.Provider.Model
	}

	rate, ok := table.Lookup(tc.Model)
	if !ok {
		return tc // tokens only; dollars unknown
	}
	tc.Priced = true
	tc.InputUSD = perMTok(tc.Tokens.Input, rate.InputPerMTok)
	tc.OutputUSD = perMTok(tc.Tokens.Output, rate.OutputPerMTok)
	tc.CacheReadUSD = perMTok(tc.Tokens.CacheRead, rate.CacheReadPerMTok)
	tc.CacheWriteUSD = perMTok(tc.Tokens.CacheWrite, rate.CacheWritePerMTok)
	tc.TotalUSD = tc.InputUSD + tc.OutputUSD + tc.CacheReadUSD + tc.CacheWriteUSD
	// Savings = what the cached-read tokens would have cost at the full input rate
	// minus what they actually cost at the cache-read rate. Never negative.
	if saved := perMTok(tc.Tokens.CacheRead, rate.InputPerMTok-rate.CacheReadPerMTok); saved > 0 {
		tc.CacheSavingsUSD = saved
	}
	return tc
}

// perMTok prices n tokens at a per-million-token rate.
func perMTok(n int, ratePerMTok float64) float64 {
	return float64(n) * ratePerMTok / 1_000_000
}
