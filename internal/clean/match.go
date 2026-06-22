package clean

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/topic"
	"github.com/tonitienda/agent-smith/schema"
)

// PreviewMatch is the natural-language `/clean "<topic>"` half of the wedge
// (AS-029, PRD §7.12, §10 Q4, D6). It resolves a free-text topic query to live
// segments with the deterministic matcher below, then builds the very same Plan
// the handle path does (Preview) — atomic tool-call/result pairing, tokens/$
// reclaimed, recency warnings — so a topic removal and a hand-picked one can
// never disagree about what leaves the window. Each directly matched item is
// annotated with why it matched (Item.Why) so the preview is explainable and the
// user can trust or trim the selection before applying (AC). It is pure: nothing
// is mutated and no provider/model call is made (§10 Q4 decision: deterministic,
// explainable, zero token cost).
func PreviewMatch(proj *projection.Projection, table *cost.Table, model string, now time.Time, query string) Plan {
	ids, why := Match(proj, query)
	p := Preview(proj, table, model, now, ids)
	if len(ids) == 0 {
		// Surface the query so the empty preview explains nothing matched, reusing
		// the same "no live segment matched" path as an unknown handle.
		if q := strings.TrimSpace(query); q != "" {
			p.Unknown = append(p.Unknown, q)
		}
		return p
	}
	for i := range p.Items {
		p.Items[i].Why = why[p.Items[i].ID]
	}
	return p
}

// match scores one block against the query terms.
type match struct {
	id    string
	score int
	terms []string // distinct query terms found, in query order
}

// Match scores the live segments of proj against a natural-language topic query
// and returns the IDs of those that match, plus a short per-ID explanation of
// why (the query terms it hit). It is the AS-029 engine: deterministic and
// explainable, with no embeddings and no provider calls (§10 Q4). Matching is
// conservative — only blocks that hit at least one significant query term are
// returned, so it prefers under-selection (the preview lets the user widen).
//
// A block's haystack is its own text and the deterministic AS-027 tags/paths/
// tool spans around it (file modules, tool names, skill/MCP/command attribution,
// reasoning, conversation, tool output) — never the model, and never raw file
// bodies, which would over-match. Results are ranked by how many distinct terms
// matched (more specific first), ties broken by ID for determinism.
func Match(proj *projection.Projection, query string) (ids []string, why map[string]string) {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}

	var matches []match
	for _, b := range proj.Blocks() {
		if !b.Live {
			continue // /clean operates on the live window only
		}
		hay := haystack(b.Block)
		if hay == "" {
			continue
		}
		var hit []string
		for _, t := range terms {
			if strings.Contains(hay, t) {
				hit = append(hit, t)
			}
		}
		if len(hit) == 0 {
			continue
		}
		matches = append(matches, match{id: b.ID, score: len(hit), terms: hit})
	}

	// More distinct terms matched ⇒ a more specific hit; rank those first. Ties
	// break on ID so the selection is stable across runs (AC: explainable).
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].id < matches[j].id
	})

	why = make(map[string]string, len(matches))
	for _, m := range matches {
		ids = append(ids, m.id)
		why[m.id] = "matched " + strings.Join(m.terms, ", ")
	}
	return ids, why
}

// haystack is the lowercased searchable text for a block: its own content plus
// the deterministic topic tags around it (AS-027). Raw file-read bodies are
// deliberately excluded — a read of an unrelated file that merely mentions a
// term would otherwise drag in (conservative under-selection, AC) — but the
// read's path/module still matters and rides in via its tag.
func haystack(b schema.Block) string {
	var sb strings.Builder
	write := func(s string) {
		if s != "" {
			sb.WriteByte(' ')
			sb.WriteString(s)
		}
	}

	if b.Text != nil {
		write(b.Text.Text)
		for _, p := range b.Text.Parts {
			write(p.Text)
		}
	}
	if b.Reasoning != nil {
		write(b.Reasoning.Text)
		for _, s := range b.Reasoning.Summary {
			write(s)
		}
	}
	if b.ToolCall != nil {
		write(b.ToolCall.Name)
		write(b.ToolCall.ToolSubtype)
		write(b.ToolCall.ArgumentsRaw)
	}
	if b.ToolResult != nil {
		write(b.ToolResult.Stdout)
		write(b.ToolResult.Stderr)
		for _, p := range b.ToolResult.Content {
			write(p.Text)
		}
	}
	// The AS-027 tags carry the file module, tool, skill/MCP, command and coarse
	// type — the "tags/file paths/tool spans" the matcher is specified to read.
	for _, t := range topic.Tags(b) {
		write(t)
	}
	return strings.ToLower(sb.String())
}

// queryTerms normalizes a natural-language query into the significant terms to
// match: lowercased, split on non-alphanumerics, stop-words and one-character
// tokens dropped, de-duplicated, query order preserved. "the content related to
// the bug we fixed" ⇒ ["bug", "fixed"].
func queryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	seen := map[string]bool{}
	var out []string
	for _, f := range fields {
		if len(f) < 2 || stopwords[f] || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// stopwords are the common filler words a topic query carries that should not
// drive selection ("the content related to …"). Kept deliberately small and
// generic — the goal is to drop noise, not to stem or understand language.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "for": true, "we": true, "it": true,
	"is": true, "was": true, "are": true, "be": true, "that": true, "this": true,
	"with": true, "about": true, "related": true, "content": true, "stuff": true,
	"from": true, "by": true, "our": true, "my": true, "all": true, "any": true,
	"some": true, "thing": true, "things": true, "around": true, "regarding": true,
}
