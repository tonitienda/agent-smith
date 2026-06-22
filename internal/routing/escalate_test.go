package routing

import "testing"

func TestEscalateRetriesOnceOnNextStrongerTier(t *testing.T) {
	var tried []Tier
	res, esc := Escalate("clean", Cheap, func(tier Tier) Attempt {
		tried = append(tried, tier)
		// Fail on cheap, succeed once escalated.
		if tier == Cheap {
			return Attempt{OK: false, Reason: "low confidence"}
		}
		return Attempt{OK: true}
	})
	if !res.OK {
		t.Errorf("final result OK = false, want true after escalation")
	}
	if esc == nil {
		t.Fatal("escalation = nil, want a record of the cheap→standard retry")
	}
	if esc.Feature != "clean" || esc.From != Cheap || esc.To != Standard || esc.Reason != "low confidence" {
		t.Errorf("escalation = %+v, want {clean cheap standard low confidence}", *esc)
	}
	if want := []Tier{Cheap, Standard}; len(tried) != 2 || tried[0] != want[0] || tried[1] != want[1] {
		t.Errorf("attempts ran on %v, want %v (exactly one retry)", tried, want)
	}
}

func TestEscalateNoRetryWhenFirstSucceeds(t *testing.T) {
	calls := 0
	res, esc := Escalate("clean", Cheap, func(Tier) Attempt {
		calls++
		return Attempt{OK: true}
	})
	if !res.OK || esc != nil || calls != 1 {
		t.Errorf("ok-first run = ok %v, esc %v, calls %d; want true, nil, 1", res.OK, esc, calls)
	}
}

func TestEscalateNoRetryAtStrongestTier(t *testing.T) {
	calls := 0
	res, esc := Escalate("clean", Strong, func(Tier) Attempt {
		calls++
		return Attempt{OK: false, Reason: "failed"}
	})
	// Already strongest: the failing result is returned as-is, no escalation.
	if res.OK || esc != nil || calls != 1 {
		t.Errorf("strongest-tier failure = ok %v, esc %v, calls %d; want false, nil, 1", res.OK, esc, calls)
	}
}
