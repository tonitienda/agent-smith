package skillrollup

import (
	"fmt"
	"sort"
	"strings"

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
}

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
	}

	out := Report{Sessions: len(allSessions), Total: total}
	for _, k := range order {
		a := groups[k]
		a.g.Sessions = len(a.sessions)
		a.g.Escalated = a.g.Sessions >= EscalateSessions
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
		return sig(gi.Kind, gi.Summary) < sig(gj.Kind, gj.Summary)
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
	return out
}

func sig(kind, summary string) string { return kind + "\x00" + summary }

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
