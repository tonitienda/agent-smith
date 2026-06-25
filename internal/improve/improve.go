// Package improve is the self-improving config layer (AS-058, PRD §7.25): it
// consolidates the cross-session findings rollup (AS-050) into a queue of
// proposed edits to memory files and skills, surfaced on demand through
// `/improve` (and `smith improve`). It generalizes the living-skills pattern —
// a remedy a single session can surface becomes a *proposal* once the same
// actionable suggestion has recurred across at least MinSessions distinct
// sessions — or, even from one session, once it is grounded strongly enough
// (Confidence ≥ HighConfidence, AS-138) — so the agent learns *your* workflow
// from evidence rather than noise.
//
// The package is deterministic and makes no model calls: it selects and narrows
// the rollup's grounded remedies, it never authors them. Proposals never
// auto-apply — every applied edit goes through a shown diff (D9, Appendix C.5),
// and every proposal is dismissible or snoozable, with the decision remembered
// across sessions via the Ledger.
package improve

import (
	"fmt"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/render"
	"github.com/tonitienda/agent-smith/internal/skillrollup"
)

// MinSessions is the recurrence threshold (AS-058 AC): the same actionable
// suggestion must surface in at least this many distinct sessions before it is
// promoted to a proposal. One session is not yet a pattern.
const MinSessions = 2

// HighConfidence is the single-session promotion threshold (AS-138): a finding
// whose grounding signal (the rediscovered-fact detector's count of failed prior
// attempts) reaches this many is proposed immediately, without waiting for a
// second session. A fact the agent only flailed once or twice over still waits
// for recurrence — one weakly grounded sighting is not yet a pattern.
const HighConfidence = 3

// Proposal is one consolidated, cross-session config-improvement suggestion: a
// single edit (a whole line) to one target file (a memory file or a skill's
// SKILL.md), grounded in the distinct sessions that raised it.
type Proposal struct {
	Index    int      // 1-based, stable across a deterministic Build
	Kind     string   // the finding kind it came from (carried for Resolve)
	Summary  string   // human-readable description of the gap
	Target   string   // memory file or skill file the edit lands in
	Edit     string   // the proposed addition (a whole line)
	Sessions int      // distinct sessions that raised it (≥ MinSessions unless high-confidence)
	Examples []string // up to a few sample session ids — evidence links

	// HighConfidence records that this proposal was promoted on a single
	// high-confidence sighting (Confidence ≥ HighConfidence) rather than on
	// cross-session recurrence, so the rendered grounding can explain why a
	// one-session fact was proposed.
	HighConfidence bool
}

// Key is a proposal's dedup identity for the dismissal Ledger: its target file
// plus the normalized edit text. Keying on the edit (not just the finding
// signature) means a remembered decision is *superseded* when the proposed edit
// changes — a refined remedy is a fresh proposal the user has not yet ruled on
// (AS-058 conflict handling), while a re-offer of the identical edit stays
// suppressed.
func Key(target, edit string) string {
	return normalize(target) + "\x00" + normalize(edit)
}

// normalize collapses runs of whitespace so a cosmetic reformat of the same edit
// does not look like a new proposal.
func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

// Build consolidates the cross-session findings rollup into the pending proposal
// queue: one proposal per finding signature that recurs across ≥ MinSessions
// distinct sessions (or is a single ≥ HighConfidence sighting), carries a target
// and an edit, and is neither already resolved
// (the remedy was applied) nor dismissed/snoozed in the ledger. The rollup
// groups are already deterministic and ordered most-recurring-first, so the
// proposal numbering is stable for `/improve apply <n>`.
func Build(rep skillrollup.Report, led *Ledger, now time.Time) []Proposal {
	var out []Proposal
	n := 0
	for _, g := range rep.Groups {
		if g.Resolved || g.Diff == "" || g.Target == "" {
			continue
		}
		// A proposal is promoted once it recurs across MinSessions distinct
		// sessions, or — even from a single session — once it is strongly enough
		// grounded (AS-138). A weakly grounded one-session fact still waits.
		if g.Sessions < MinSessions && g.Confidence < HighConfidence {
			continue
		}
		if led != nil && led.Suppressed(Key(g.Target, g.Diff), now) {
			continue
		}
		n++
		out = append(out, Proposal{
			Index:    n,
			Kind:     g.Kind,
			Summary:  g.Summary,
			Target:   g.Target,
			Edit:     g.Diff,
			Sessions: g.Sessions,
			Examples: g.Examples,
			// A finding that passed the gate with fewer than MinSessions distinct
			// sessions can only have been promoted on confidence, so this both
			// matches the field's doc and stays a single source of truth.
			HighConfidence: g.Sessions < MinSessions,
		})
	}
	return out
}

// Render formats the pending proposal queue for /improve and `smith improve`. It
// is face-agnostic, so the TUI panel and the headless subcommand show the same
// view. Each proposal shows its grounding (how many sessions, sample ids), the
// target file and the line that would land, and the accept/dismiss/snooze
// commands.
func Render(props []Proposal) string {
	var b strings.Builder
	b.WriteString("Self-improving config — proposed edits\n\n")
	if len(props) == 0 {
		b.WriteString("  No proposals yet. A suggestion is proposed once the same\n")
		fmt.Fprintf(&b, "  actionable gap recurs across %s or more.\n", render.Count(MinSessions, "session"))
		return strings.TrimRight(b.String(), "\n")
	}
	for _, p := range props {
		fmt.Fprintf(&b, "  %d. %s\n", p.Index, p.Summary)
		fmt.Fprintf(&b, "     %s: %s\n", p.Target, p.Edit)
		ev := "seen across " + render.Count(p.Sessions, "session")
		if p.HighConfidence {
			ev = "high-confidence single fact"
		}
		if len(p.Examples) > 0 {
			ev += " — e.g. " + strings.Join(p.Examples, ", ")
		}
		fmt.Fprintf(&b, "     %s\n", ev)
		fmt.Fprintf(&b, "     apply: /improve apply %d · dismiss: /improve dismiss %d · snooze: /improve snooze %d\n",
			p.Index, p.Index, p.Index)
	}
	return strings.TrimRight(b.String(), "\n")
}
