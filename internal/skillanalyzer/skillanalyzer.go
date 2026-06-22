// Package skillanalyzer is the predict-then-measure half of living skills
// (AS-049, PRD §7.20, Decision Log D7, Appendix C.1–C.2). At skill load it
// establishes each skill's expectation contract — the declared C.1 contract when
// the skill carries one, otherwise one *inferred* from the skill's stated purpose
// (Q8 resolution) — and freezes it. At session teardown it compares the contract
// against the skill's measured span (via skillcontract.Tracker) and grades the
// activation: a verdict, a normalized score, a classification, grounded evidence,
// and a remedy with a concrete diff (Appendix C.2).
//
// Grounded, never vibes (§7.20): every grade is computed from the log and cites
// turns, cost, and a jump-to span link; the contract is fixed at load time, not
// hindsight (the §9 fairness mitigation), so a skill is never judged against
// expectations invented after seeing the trace. Like the rediscovered-fact
// detector (AS-048) and the /insights writer (AS-045), grading is deterministic
// and makes no model calls, so the analyzer is free when idle and within budget
// when enabled; a model-assisted prose layer is deferred (the same split insights
// uses for AS-109). It is explicitly experimental and opt-in (D7 demotes it until
// session volume exists): EnabledByDefault is false.
//
// Layering: this package consumes subagent, skillcontract, and schema and points
// inward, the same way the other analyzers sit below the loop (see
// docs/architecture/package-contracts.md). The composition root adapts discovered
// skills into the Skill catalog, keeping this package free of the skill loader.
package skillanalyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/skillcontract"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// Name is the built-in sub-agent's stable registry name.
const Name = "skill-expectation-analyzer"

// FindingKind tags every grade this analyzer emits, so /insights (AS-045) and the
// /skills rollup (AS-050) recognize a skill grade without re-running the analysis.
const FindingKind = "skill_grade"

// Verdict is the per-activation outcome (Appendix C.2).
type Verdict string

const (
	// Helped: the skill did meaningful work within its contract.
	Helped Verdict = "helped"
	// NoOp: the skill was loaded but produced no measurable work.
	NoOp Verdict = "no_op"
	// Underperformed: the skill worked but missed its contract (a fact it should
	// have encoded was rediscovered, or it blew its effort budget).
	Underperformed Verdict = "underperformed"
	// ShouldHaveLoaded: a matching skill was available but never triggered.
	ShouldHaveLoaded Verdict = "should_have_loaded"
)

// Classification is the root cause behind a non-helped verdict (Appendix C.2).
type Classification string

const (
	// ContentGap: the skill's instructions lacked a fact the session rediscovered.
	ContentGap Classification = "content_gap"
	// TriggerFailure: the skill's description/triggers failed to fire when relevant.
	TriggerFailure Classification = "trigger_failure"
	// Friction: the skill loaded but added cost without payoff (no_op or over budget).
	Friction Classification = "friction"
)

// Remedy is the suggested fix attached to a non-helped grade (Appendix C.2).
type Remedy string

const (
	// PatchSkill: add the rediscovered fact to the skill body.
	PatchSkill Remedy = "patch_skill"
	// FixDescription: improve the skill's description/triggers.
	FixDescription Remedy = "fix_description"
	// Prune: the skill earned its cost nowhere; consider removing it.
	Prune Remedy = "prune"
	// NewSkill: seed a new skill for an unmet need.
	NewSkill Remedy = "new_skill"
)

// Skill is one available skill the analyzer grades, adapted by the composition
// root from a discovered skill (keeping this package free of the skill loader).
// Frontmatter is the raw C.1 contract text (parsed once at New); Source is the
// SKILL.md path a patch_skill/fix_description remedy targets.
type Skill struct {
	Name        string
	Description string
	Frontmatter string
	Source      string
}

// Evidence is the measured, citable backing for a grade (§7.20: turns, cost, and
// a transcript span). Seqs are the block sequence numbers the skill produced —
// the jump-to anchors a face scrolls to.
type Evidence struct {
	Turns        int
	ToolCalls    int
	CostUSD      float64
	Rediscovered []string
	Seqs         []int
}

// Grade is the C.2 result for one skill: its verdict, normalized score (0..1),
// root-cause classification, evidence, remedy, and a concrete one-line diff
// (empty for a clean "helped"). EvalSeed is the session://… span link attaching
// the trace as the first regression case (C.2).
type Grade struct {
	Skill          string
	Verdict        Verdict
	Score          float64
	Classification Classification
	Evidence       Evidence
	Remedy         Remedy
	Diff           string
	Target         string
	EvalSeed       string
}

// state is a skill's frozen contract: parsed/inferred once at New and never
// changed thereafter (the fairness property — the contract is fixed at load time,
// not hindsight). Inferred reports whether the contract was synthesized from the
// description because the skill declared none.
type state struct {
	skill    Skill
	contract skillcontract.Contract
	inferred bool
}

// Analyzer grades a session's skill activations against their load-time contracts.
// The contracts are established and frozen at construction, so a grade can never
// be judged against an expectation invented after seeing the trace.
type Analyzer struct {
	states []state
}

// New builds an Analyzer over a skill catalog, establishing each skill's contract
// once: the declared C.1 contract when the skill carries one, otherwise a contract
// inferred from its stated purpose. The contracts are frozen here and immutable
// for the Analyzer's life (Appendix C.2 fairness property).
func New(catalog []Skill) *Analyzer {
	states := make([]state, 0, len(catalog))
	for _, s := range catalog {
		c := skillcontract.ParseContract(s.Frontmatter)
		inferred := !c.Declared
		if inferred {
			c = inferContract(s)
		}
		states = append(states, state{skill: s, contract: c, inferred: inferred})
	}
	return &Analyzer{states: states}
}

// inferContract synthesizes a contract from a skill's stated purpose (Q8): the
// description becomes the expected-outcome summary only. It deliberately seeds no
// should-not-rediscover facts — a one-line description names no specific fact the
// skill encodes, and treating its individual words as facts would flag a skill as
// underperforming the moment it uses any word from its own description (a severe
// false positive). content_gap grading therefore fires only for a skill that
// *declares* explicit should_not_rediscover facts (D0/D7: no fabricated
// expectations, precision over recall). No effort budget is invented either.
func inferContract(s Skill) skillcontract.Contract {
	return skillcontract.Contract{
		Declared:        false,
		ExpectedOutcome: skillcontract.ExpectedOutcome{Summary: strings.TrimSpace(s.Description)},
	}
}

// Contracts returns a copy of the frozen per-skill contracts (Appendix C.2: the
// inferred contracts are recorded at load time and immutable thereafter — this is
// the testable accessor for that property).
func (a *Analyzer) Contracts() map[string]skillcontract.Contract {
	out := make(map[string]skillcontract.Contract, len(a.states))
	for _, st := range a.states {
		out[st.skill.Name] = st.contract
	}
	return out
}

// Inferred reports whether the named skill's contract was inferred (true) or
// declared (false). A skill not in the catalog reports false.
func (a *Analyzer) Inferred(name string) bool {
	for _, st := range a.states {
		if st.skill.Name == name {
			return st.inferred
		}
	}
	return false
}

// Factory yields fresh Analyzers over the same catalog, so every session grades
// against the same frozen contracts while still getting its own (stateless)
// instance, per the framework's per-session instancing rule.
func Factory(catalog []Skill) subagent.Factory {
	return func() subagent.SubAgent { return New(catalog) }
}

// Manifest declares the analyzer: a passive, session-end, session-scoped analyzer
// that makes no model calls (zero cost even when enabled) and is opt-in —
// EnabledByDefault is false because D7 demotes it until session volume exists.
func (a *Analyzer) Manifest() subagent.Manifest {
	return subagent.Manifest{
		Name:             Name,
		Kind:             subagent.KindAnalyzer,
		Schedule:         subagent.AtSessionEnd,
		Scope:            subagent.SessionScope,
		EnabledByDefault: false, // experimental, opt-in (D7)
		ModelTier:        "",    // deterministic grading → zero cost
		BudgetUSD:        0,
		Emits:            []string{FindingKind},
		Permissions:      []subagent.Permission{subagent.ReadTranscript, subagent.ProposeEdit},
	}
}

// Init is a no-op: grading happens in Teardown over the handed slice.
func (a *Analyzer) Init(subagent.Scope) {}

// Observe is a no-op: the analyzer grades the slice handed to Teardown rather than
// accumulating per block, so it adds no per-block work to a turn.
func (a *Analyzer) Observe(schema.Block) {}

// Teardown grades the session's skill activations and returns one finding per
// grade. It spends nothing (no model calls), so it is never budget-capped.
func (a *Analyzer) Teardown(scope subagent.Scope, slice []schema.Block) subagent.Result {
	grades := a.Evaluate(slice, scope.Session)
	findings := make([]subagent.Finding, 0, len(grades))
	for _, g := range grades {
		findings = append(findings, finding(g))
	}
	return subagent.Result{Findings: findings}
}

// Evaluate grades every skill against its frozen contract over the session slice:
// one grade per activated skill (aggregating its spans), plus a should_have_loaded
// grade for any available-but-never-activated skill whose stated purpose the
// session matched. It is the pure core Teardown wraps, exported for direct testing.
func (a *Analyzer) Evaluate(slice []schema.Block, session string) []Grade {
	tracker := skillcontract.NewTracker()
	for _, st := range a.states {
		tracker.Declare(st.skill.Name, st.contract)
	}
	for _, b := range slice {
		tracker.Observe(b)
	}
	spans := tracker.Finish()

	footprint := footprints(slice)            // per-skill attributed text + anchors
	sessionTokens := tokenSet(allText(slice)) // computed once for the should_have_loaded scan

	// Aggregate spans per skill so a skill used twice yields one clean grade.
	agg := map[string]skillcontract.Actuals{}
	activated := map[string]bool{}
	var order []string
	for _, s := range spans {
		if !activated[s.Skill] {
			order = append(order, s.Skill)
		}
		activated[s.Skill] = true
		ag := agg[s.Skill]
		ag.ToolCalls += s.Actuals.ToolCalls
		ag.Turns += s.Actuals.Turns
		ag.CostUSD += s.Actuals.CostUSD
		agg[s.Skill] = ag
	}

	var out []Grade
	for _, name := range order {
		st := a.stateOf(name)
		if st == nil {
			continue // a span for a skill not in the catalog: nothing to grade against
		}
		out = append(out, gradeActivation(*st, agg[name], footprint[name], session))
	}
	// should_have_loaded: a catalog skill that never activated but whose purpose the
	// session matched (trigger failure).
	for _, st := range a.states {
		if activated[st.skill.Name] {
			continue
		}
		if seq, ok := matchedButIdle(st.skill, sessionTokens, slice); ok {
			out = append(out, missedLoad(st.skill, seq, session))
		}
	}
	return out
}

func (a *Analyzer) stateOf(name string) *state {
	for i := range a.states {
		if a.states[i].skill.Name == name {
			return &a.states[i]
		}
	}
	return nil
}

// gradeActivation grades one activated skill from its aggregated actuals and the
// text it produced. Precision order: no work → no_op/friction/prune; a contract
// fact rediscovered → underperformed/content_gap/patch_skill; an effort budget
// blown → underperformed/friction/fix_description; otherwise helped.
func gradeActivation(st state, act skillcontract.Actuals, fp footprint, session string) Grade {
	g := Grade{
		Skill:    st.skill.Name,
		Target:   st.skill.Source,
		EvalSeed: evalSeed(session, fp.firstSeq()),
		Evidence: Evidence{
			Turns:     act.Turns,
			ToolCalls: act.ToolCalls,
			CostUSD:   act.CostUSD,
			Seqs:      fp.seqs,
		},
	}

	if act.ToolCalls == 0 && act.Turns == 0 {
		g.Verdict, g.Classification, g.Remedy, g.Score = NoOp, Friction, Prune, 0
		g.Diff = fmt.Sprintf("- consider pruning `%s` — loaded but did no measurable work this session", st.skill.Name)
		return g
	}

	if redis := rediscovered(st.contract, fp.text); len(redis) > 0 {
		g.Evidence.Rediscovered = redis
		g.Verdict, g.Classification, g.Remedy, g.Score = Underperformed, ContentGap, PatchSkill, 0.3
		g.Diff = fmt.Sprintf("+ encode in `%s`: %s — rediscovered despite the skill being loaded", st.skill.Name, strings.Join(redis, "; "))
		return g
	}

	if over, budget := overBudget(st.contract, act); over {
		g.Verdict, g.Classification, g.Remedy, g.Score = Underperformed, Friction, FixDescription, 0.5
		g.Diff = fmt.Sprintf("~ tighten `%s` guidance — %d tool calls vs budget %d", st.skill.Name, act.ToolCalls, budget)
		return g
	}

	g.Verdict, g.Score = Helped, 1.0
	return g
}

// missedLoad builds a should_have_loaded grade for a skill the session needed but
// never triggered, anchored to the block where the match showed up.
func missedLoad(s Skill, seq int, session string) Grade {
	return Grade{
		Skill:          s.Name,
		Verdict:        ShouldHaveLoaded,
		Score:          0,
		Classification: TriggerFailure,
		Remedy:         FixDescription,
		Target:         s.Source,
		EvalSeed:       evalSeed(session, seq),
		Evidence:       Evidence{Seqs: []int{seq}},
		Diff:           fmt.Sprintf("~ sharpen `%s` triggers/description — it matched this session's work but never loaded", s.Name),
	}
}

// rediscovered returns the contract's should-not-rediscover entries that surface
// in the skill's own produced text — a grounded content-gap signal: the skill
// claims to encode the fact, yet it was re-derived while the skill was active. A
// phrase counts as rediscovered only when most of its significant terms appear
// (matchPhrase), so a single coincidental common word ("make", "command") cannot
// trigger a false content gap.
func rediscovered(c skillcontract.Contract, text string) []string {
	if text == "" {
		return nil
	}
	have := tokenSet(text)
	var out []string
	for _, phrase := range c.ExpectedOutcome.ShouldNotRediscover {
		if matchPhrase(phrase, have) {
			out = append(out, phrase)
		}
	}
	return out
}

// rediscoverThreshold is the fraction of a fact phrase's significant terms that
// must appear in the text to call it rediscovered — high enough that one shared
// common word does not fire, low enough that a fact stated in different words
// ("ran make ship to deploy" vs "the deploy command is make ship") still matches.
const rediscoverThreshold = 0.75

// matchPhrase reports whether at least rediscoverThreshold of a phrase's
// significant terms are present in have.
func matchPhrase(phrase string, have map[string]bool) bool {
	terms := significantTerms(phrase)
	if len(terms) == 0 {
		return false
	}
	matched := 0
	for _, t := range terms {
		if have[t] {
			matched++
		}
	}
	return float64(matched)/float64(len(terms)) >= rediscoverThreshold
}

// overBudget reports whether actuals blew the declared effort budget by more than
// half (a soft target, never a gate — only a clear overrun grades down). It
// returns the tool-call budget for the diff. A zero budget is unspecified.
func overBudget(c skillcontract.Contract, act skillcontract.Actuals) (bool, int) {
	b := c.ExpectedOutcome.EffortBudget.ToolCalls
	if b > 0 && act.ToolCalls > b+b/2 {
		return true, b
	}
	return false, b
}

// matchedButIdle reports whether the session's work matched a skill's stated
// purpose (a significant term of its name appears in the pre-computed session
// token set), returning the first block sequence where the match shows so the
// grade carries a jump-to link. It keys on name terms (not the looser
// description) to keep should_have_loaded to the high-precision bar D7 demands.
// The session token set is computed once by the caller, not per skill.
func matchedButIdle(s Skill, sessionTokens map[string]bool, slice []schema.Block) (int, bool) {
	terms := significantTerms(s.Name)
	if len(terms) == 0 || !sharesToken(terms, sessionTokens) {
		return 0, false
	}
	for _, b := range slice {
		if sharesToken(terms, tokenSet(blockText(b))) {
			return b.Seq, true
		}
	}
	return 0, true
}

// footprint is a skill's measured trace: the text it produced and the block
// sequence numbers where it appeared (jump-to anchors).
type footprint struct {
	text string
	seqs []int
}

func (f footprint) firstSeq() int {
	if len(f.seqs) == 0 {
		return 0
	}
	return f.seqs[0]
}

// footprints indexes each skill's produced text and anchors from blocks attributed
// to it (other than the availability marker), so a grade can cite what the skill
// actually did and where.
func footprints(slice []schema.Block) map[string]footprint {
	out := map[string]footprint{}
	for _, b := range slice {
		if b.Attribution == nil || b.Attribution.Skill == "" || b.Kind == eventlog.KindSkillLoad {
			continue
		}
		name := b.Attribution.Skill
		fp := out[name]
		if t := blockText(b); t != "" {
			fp.text += "\n" + t
		}
		fp.seqs = append(fp.seqs, b.Seq)
		out[name] = fp
	}
	return out
}

// finding turns a grade into a propose-only Finding: a one-line verdict summary, a
// grounded detail block (score, classification, evidence, eval-seed link), and a
// concrete diff proposal when the grade carries a remedy.
func finding(g Grade) subagent.Finding {
	f := subagent.Finding{
		Kind:    FindingKind,
		Summary: fmt.Sprintf("skill `%s`: %s", g.Skill, g.Verdict),
		Detail:  detail(g),
	}
	if g.Diff != "" {
		target := g.Target
		if target == "" {
			target = g.Skill
		}
		f.Proposal = &subagent.Edit{Target: target, Description: g.Diff}
	}
	return f
}

// detail renders the grounded C.2 trace behind a grade.
func detail(g Grade) string {
	var b strings.Builder
	fmt.Fprintf(&b, "verdict %s (score %.2f)", g.Verdict, g.Score)
	if g.Classification != "" {
		fmt.Fprintf(&b, ", %s", g.Classification)
	}
	if g.Remedy != "" {
		fmt.Fprintf(&b, "; remedy %s", g.Remedy)
	}
	if g.Verdict != ShouldHaveLoaded {
		fmt.Fprintf(&b, "\nmeasured: %d turns, %d tool calls, $%.4f", g.Evidence.Turns, g.Evidence.ToolCalls, g.Evidence.CostUSD)
	}
	if len(g.Evidence.Rediscovered) > 0 {
		fmt.Fprintf(&b, "\nrediscovered: %s", strings.Join(g.Evidence.Rediscovered, "; "))
	}
	if len(g.Evidence.Seqs) > 0 {
		fmt.Fprintf(&b, "\nblocks: %s", anchors(g.Evidence.Seqs))
	}
	if g.EvalSeed != "" {
		fmt.Fprintf(&b, "\neval_seed: %s", g.EvalSeed)
	}
	return b.String()
}

// evalSeed builds the session://session#seq jump-to link (C.2). A zero seq yields
// the session-level link.
func evalSeed(session string, seq int) string {
	if session == "" {
		return ""
	}
	if seq <= 0 {
		return "session://" + session
	}
	return fmt.Sprintf("session://%s#%d", session, seq)
}

// anchors renders block sequence numbers as #-prefixed jump-to anchors.
func anchors(seqs []int) string {
	parts := make([]string, len(seqs))
	for i, s := range seqs {
		parts[i] = fmt.Sprintf("#%d", s)
	}
	return strings.Join(parts, " ")
}

// blockText is the human-readable text of a block for matching: an assistant/user
// text body and a tool result's stdout/stderr/content.
func blockText(b schema.Block) string {
	var sb strings.Builder
	if b.Text != nil {
		sb.WriteString(b.Text.Text)
	}
	if r := b.ToolResult; r != nil {
		sb.WriteByte('\n')
		sb.WriteString(r.Stdout)
		sb.WriteByte('\n')
		sb.WriteString(r.Stderr)
		for _, p := range r.Content {
			if p.Text != "" {
				sb.WriteByte('\n')
				sb.WriteString(p.Text)
			}
		}
	}
	return sb.String()
}

// allText concatenates every block's text for the session-wide match scan.
func allText(slice []schema.Block) string {
	var sb strings.Builder
	for _, b := range slice {
		if t := blockText(b); t != "" {
			sb.WriteString(t)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// stopwords are common words that carry no identifying signal, dropped so a
// shared term is a meaningful link rather than filler. Length alone cannot exclude
// them ("the", "command" survive the >= 3 rule), and a stray match on one would
// pollute both the rediscovery threshold and the should_have_loaded scan.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"this": true, "are": true, "was": true, "has": true, "have": true,
	"its": true, "into": true, "from": true, "via": true, "use": true,
	"using": true, "run": true, "command": true, "commands": true,
}

// significantTerms splits a phrase into lowercased alphanumeric tokens of length
// >= 3 that are not stopwords, so a shared term is a meaningful identifier rather
// than punctuation or filler.
func significantTerms(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(w) >= 3 && !stopwords[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

// tokenSet returns the significant-term set of a text.
func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range significantTerms(s) {
		set[t] = true
	}
	return set
}

// sharesToken reports whether any term is present in the set.
func sharesToken(terms []string, set map[string]bool) bool {
	for _, t := range terms {
		if set[t] {
			return true
		}
	}
	return false
}
