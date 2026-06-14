package command

import (
	"strings"
	"unicode/utf8"
)

// fuzzyScore reports whether query is a subsequence of name (case-folded by the
// caller) and, if so, a relevance score — higher is better. The score rewards
// exact and prefix matches and contiguous runs, so "co" ranks /cost above
// /context only when it should and an exact "cost" wins outright. ok is false
// when query is not a subsequence of name at all, so the palette hides misses.
func fuzzyScore(query, name string) (score int, ok bool) {
	name = strings.ToLower(name)
	switch {
	case query == name:
		return 1000, true
	case strings.HasPrefix(name, query):
		// Prefix match: shorter remainders rank higher (closer to exact).
		return 500 - (len(name) - len(query)), true
	case strings.Contains(name, query):
		return 200 - strings.Index(name, query), true
	}

	// Subsequence match: every query char appears in order. Reward adjacency so
	// "ct" still finds "context" but ranks below a contiguous hit.
	qi := 0
	run := 0
	for ni := 0; ni < len(name) && qi < len(query); ni++ {
		if name[ni] == query[qi] {
			qi++
			run++
			score += run // contiguous matches compound
		} else {
			run = 0
		}
	}
	if qi != len(query) {
		return 0, false
	}
	return score, true
}

// nearest returns the candidate with the smallest edit distance to name, used
// for "did you mean …?". It only suggests when the distance is small relative to
// the name's length, so a wildly different typo yields no (misleading) hint.
func nearest(name string, candidates []string) (string, bool) {
	best := ""
	bestDist := -1
	for _, c := range candidates {
		d := levenshtein(name, strings.ToLower(c))
		if bestDist < 0 || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best == "" {
		return "", false
	}
	// Accept only reasonably close matches: at most a third of the longer name's
	// length (rounded up), and never more than 3 edits. Count runes so the bound
	// matches levenshtein's rune-based distance for non-ASCII names.
	limit := (max(utf8.RuneCountInString(name), utf8.RuneCountInString(best)) + 2) / 3
	if limit > 3 {
		limit = 3
	}
	if bestDist > limit {
		return "", false
	}
	return best, true
}

// levenshtein returns the edit distance between a and b (single-row DP). It
// works in runes, not bytes, so a multi-byte character counts as one edit and
// distances stay correct for non-ASCII command names.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(rb)]
}
