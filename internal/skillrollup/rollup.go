package skillrollup

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/render"
	"github.com/tonitienda/agent-smith/internal/subagent"
)

// Report is the cross-session aggregation /skills renders: the distinct sessions
// and total findings observed for the project, the recurring findings (grouped by
// signature, most-recurring first, with the 3+-session escalation flagged), and
// the unresolved remedies numbered for `/skills apply <n>`.
type Report struct {
	Sessions int     // distinct sessions in the corpus
	Total    int     // total findings (tombstones excluded)
	Groups   []Group // recurring findings, most distinct sessions first
	Pending  []Pending
	Efficacy []Efficacy // before/after payoff of each applied remedy (AS-139)
}

// Efficacy is the before/after view of one applied remedy's payoff (AS-139): the
// finding signature it resolved, when the remedy was applied (the tombstone's
// timestamp), and how often the same finding was recorded before vs after that
// moment. After == 0 is the deterministic proxy that the edit worked — the
// targeted fact stopped recurring once the proposal landed.
type Efficacy struct {
	Kind          string
	Summary       string
	Target        string    // the applied proposal's target file, when the marker carries it
	AppliedAt     time.Time // when the remedy was applied (earliest tombstone)
	Before        int       // findings of this signature recorded at or before AppliedAt
	After         int       // findings recorded after AppliedAt — recurrences despite the edit
	SessionsAfter int       // distinct sessions that re-raised the finding after application
}

// Group is one finding signature aggregated across sessions: how many sessions
// raised it, how often in total, whether it has crossed the escalation threshold,
// and whether its remedy has been applied (resolved).
type Group struct {
	Kind      string
	Summary   string
	SubAgent  string
	Sessions  int
	Count     int
	Escalated bool
	Resolved  bool
	Target    string
	Diff      string
	// Confidence is the strongest grounding signal seen across the group's records
	// (max, not sum) — the count of failed prior attempts that justify the fact.
	// It lets a single high-confidence finding be promoted to a proposal without
	// waiting for cross-session recurrence (AS-138).
	Confidence int
	// Examples lists up to maxExamples distinct session IDs that raised this
	// finding, sorted for determinism. It lets a portfolio view link a recurring
	// item back to concrete sessions to inspect (AS-057). Empty for findings
	// recorded without a session id.
	Examples []string
}

// maxExamples caps the session ids carried per group so a finding seen in many
// sessions stays a short, linkable sample rather than an unbounded list.
const maxExamples = 3

// Pending is an unresolved finding carrying a remedy, numbered (1-based) for
// `/skills apply <n>`. The number is stable because Rollup is deterministic.
type Pending struct {
	Index   int
	Kind    string
	Summary string
	Target  string
	Diff    string
}

// Rollup aggregates the whole findings log into the cross-session report. Findings
// are grouped by (kind, summary); a group resolved by a tombstone is marked
// resolved and never pends; a group seen in EscalateSessions or more distinct
// sessions is escalated. Groups sort by distinct sessions then total count, with
// the signature as a stable final tie-break so the rollup (and the apply numbers
// it drives) is reproducible.
func (s *Store) Rollup() Report {
	s.mu.Lock()
	records := make([]Record, len(s.all))
	copy(records, s.all)
	s.mu.Unlock()

	type acc struct {
		g        Group
		sessions map[string]bool
	}
	resolved := map[string]bool{}
	for _, r := range records {
		if r.Resolved {
			resolved[sig(r.Kind, r.Summary)] = true
		}
	}

	groups := map[string]*acc{}
	var order []string
	allSessions := map[string]bool{}
	total := 0
	for _, r := range records {
		if r.Resolved {
			continue
		}
		total++
		if r.Session != "" {
			allSessions[r.Session] = true
		}
		k := sig(r.Kind, r.Summary)
		a := groups[k]
		if a == nil {
			a = &acc{
				g:        Group{Kind: r.Kind, Summary: r.Summary, SubAgent: r.SubAgent, Resolved: resolved[k]},
				sessions: map[string]bool{},
			}
			groups[k] = a
			order = append(order, k)
		}
		a.g.Count++
		if r.Session != "" {
			a.sessions[r.Session] = true
		}
		// Keep the latest non-empty remedy so an applied/refined diff wins.
		if r.Diff != "" {
			a.g.Diff = r.Diff
		}
		if r.Target != "" {
			a.g.Target = r.Target
		}
		// Keep the strongest grounding the fact ever showed, so a single
		// high-confidence sighting is not diluted by weaker later ones.
		if r.Confidence > a.g.Confidence {
			a.g.Confidence = r.Confidence
		}
	}

	out := Report{Sessions: len(allSessions), Total: total}
	for _, k := range order {
		a := groups[k]
		a.g.Sessions = len(a.sessions)
		a.g.Escalated = a.g.Sessions >= EscalateSessions
		a.g.Examples = exampleSessions(a.sessions)
		out.Groups = append(out.Groups, a.g)
	}
	sort.SliceStable(out.Groups, func(i, j int) bool {
		gi, gj := out.Groups[i], out.Groups[j]
		if gi.Sessions != gj.Sessions {
			return gi.Sessions > gj.Sessions
		}
		if gi.Count != gj.Count {
			return gi.Count > gj.Count
		}
		if gi.Kind != gj.Kind {
			return gi.Kind < gj.Kind
		}
		return gi.Summary < gj.Summary
	})

	n := 0
	for _, g := range out.Groups {
		if g.Resolved || g.Diff == "" {
			continue
		}
		n++
		out.Pending = append(out.Pending, Pending{
			Index: n, Kind: g.Kind, Summary: g.Summary, Target: g.Target, Diff: g.Diff,
		})
	}
	out.Efficacy = efficacyFrom(records)
	return out
}

// efficacyFrom computes the before/after payoff of each applied remedy from the
// raw record log (AS-139). The application moment is the earliest tombstone for a
// signature; findings of that signature are then split into those recorded at or
// before that moment (Before) and those recorded after it (After) — a post-apply
// recurrence the edit failed to prevent. Only signatures that were actually
// applied (have a tombstone) appear. Output is sorted by application time then
// signature so the view is reproducible.
func efficacyFrom(records []Record) []Efficacy {
	applied := map[string]time.Time{} // sig → earliest application time
	kindSummary := map[string][2]string{}
	target := map[string]string{}
	for _, r := range records {
		if !r.Resolved {
			continue
		}
		k := sig(r.Kind, r.Summary)
		if t, ok := applied[k]; !ok || r.RecordedAt.Before(t) {
			applied[k] = r.RecordedAt
		}
		kindSummary[k] = [2]string{r.Kind, r.Summary}
		if r.Target != "" && target[k] == "" {
			target[k] = r.Target // the marker carries the applied proposal's target
		}
	}
	if len(applied) == 0 {
		return nil
	}

	type acc struct {
		before, after int
		sessionsAfter map[string]bool
	}
	accs := map[string]*acc{}
	for _, r := range records {
		if r.Resolved {
			continue
		}
		k := sig(r.Kind, r.Summary)
		at, ok := applied[k]
		if !ok {
			continue
		}
		a := accs[k]
		if a == nil {
			a = &acc{sessionsAfter: map[string]bool{}}
			accs[k] = a
		}
		if r.RecordedAt.After(at) {
			a.after++
			if r.Session != "" {
				a.sessionsAfter[r.Session] = true
			}
		} else {
			a.before++
		}
		if r.Target != "" && target[k] == "" {
			target[k] = r.Target // fall back to a finding's target when the marker lacks one
		}
	}

	out := make([]Efficacy, 0, len(applied))
	for k, at := range applied {
		ks := kindSummary[k]
		e := Efficacy{Kind: ks[0], Summary: ks[1], Target: target[k], AppliedAt: at}
		if a := accs[k]; a != nil {
			e.Before, e.After, e.SessionsAfter = a.before, a.after, len(a.sessionsAfter)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AppliedAt.Equal(out[j].AppliedAt) {
			return out[i].AppliedAt.Before(out[j].AppliedAt)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Summary < out[j].Summary
	})
	return out
}

func sig(kind, summary string) string { return kind + "\x00" + summary }

// exampleSessions returns up to maxExamples session ids from the set, sorted so
// the rollup stays reproducible.
func exampleSessions(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > maxExamples {
		ids = ids[:maxExamples]
	}
	return ids
}

// Render formats the /skills report: the current session's findings first (the
// per-session view), then the cross-session rollup with escalations flagged, then
// the numbered pending remedies. perSession is the current session's findings
// (from Store.Findings); session is its id, shown for context. It is face-agnostic
// so the TUI panel and a headless `smith skills` render the same view.
func Render(r Report, perSession []subagent.Finding, session string) string {
	var b strings.Builder
	b.WriteString("Skills & findings\n\n")

	label := session
	if label == "" {
		label = "current session"
	}
	fmt.Fprintf(&b, "This session (%s)\n", label)
	if len(perSession) == 0 {
		b.WriteString("  No findings recorded yet.\n")
	}
	for _, f := range perSession {
		fmt.Fprintf(&b, "  • %s\n", f.Summary)
		if f.Detail != "" {
			fmt.Fprintf(&b, "    %s\n", firstLine(f.Detail))
		}
	}

	fmt.Fprintf(&b, "\nAcross %s · %s\n", render.Count(r.Sessions, "session"), render.Count(r.Total, "finding"))
	if len(r.Groups) == 0 {
		b.WriteString("  Nothing recurring yet.\n")
	}
	for _, g := range r.Groups {
		mark := ""
		if g.Escalated {
			mark = "  ⚠ escalated"
		}
		if g.Resolved {
			mark += "  ✓ resolved"
		}
		fmt.Fprintf(&b, "  %s — %s in %s%s\n",
			g.Summary, render.Count(g.Count, "time"), render.Count(g.Sessions, "session"), mark)
	}

	if len(r.Pending) > 0 {
		b.WriteString("\nPending remedies\n")
		for _, p := range r.Pending {
			fmt.Fprintf(&b, "  %d. %s\n", p.Index, p.Summary)
			if p.Diff != "" {
				fmt.Fprintf(&b, "     %s: %s\n", targetLabel(p.Target), p.Diff)
			}
			fmt.Fprintf(&b, "     apply: /skills apply %d\n", p.Index)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// firstLine returns the first line of a detail block, so the per-session view
// stays one line per finding rather than spilling the full grounded trace.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// targetLabel names a remedy's target file, or a neutral label when none was
// resolved.
func targetLabel(t string) string {
	if t == "" {
		return "target"
	}
	return t
}
