package routing

// Auto-escalation (AS-110, PRD §7.15 "auto-escalate on failure"): when a
// tier-declared task returns a structured low-confidence/failed result, retry it
// once on the next stronger tier. This is explicit and feature-owned — never an
// invisible retry for normal interactive chat turns (AS-042 clarified decision):
// a feature opts in by routing its attempt through Escalate. The escalation is
// returned as a record so the caller can log it with its reason; surfacing those
// records in /route and /cost, and wiring the first structured-result producer,
// is tracked in AS-116.

// Attempt is one tier-bound run of an escalation-capable, tier-declared task. It
// reports OK=false (a structured low-confidence or failed result) to request one
// escalation to the next stronger tier, with Reason explaining why for the log.
type Attempt struct {
	OK     bool
	Reason string
}

// Escalation records that a tier-declared task retried on a stronger tier: which
// feature, the tiers it moved between, and why. The caller logs it so the
// escalation is visible (never an invisible retry) and attributable.
type Escalation struct {
	Feature string
	From    Tier
	To      Tier
	Reason  string
}

// Escalate runs attempt on start; if it reports a structured low-confidence/failed
// result (OK=false) and a stronger tier exists, it retries once on that tier and
// returns the retry's outcome alongside a non-nil Escalation describing the move.
// When start already succeeds, or is already the strongest tier, attempt runs
// exactly once and the returned *Escalation is nil. The retry is single — one
// step up the ladder, not an unbounded climb — matching the AC "retries once on
// the next stronger tier".
func Escalate(feature string, start Tier, attempt func(Tier) Attempt) (Attempt, *Escalation) {
	res := attempt(start)
	if res.OK {
		return res, nil
	}
	next, ok := NextTier(start)
	if !ok {
		return res, nil // already the strongest tier; nothing to escalate to
	}
	esc := &Escalation{Feature: feature, From: start, To: next, Reason: res.Reason}
	return attempt(next), esc
}
