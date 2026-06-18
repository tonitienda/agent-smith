// Package composition computes what is actually occupying the model's context
// window, broken down segment by segment — the data behind the /context view
// (AS-026, PRD §7.11), the flagship "see your window" differentiator.
//
// It is a pure projection over the projection engine's live blocks (AS-006):
// each live block becomes a Segment carrying the mechanically derivable
// dimensions — type, origin, token share, dollar cost, recency and selection
// ID. No model calls; the panel opens instantly from data already on the log.
//
// The token figures are the per-block estimates from internal/cost (AS-063):
// providers report usage per turn, not per block, so a heuristic is the only
// way to attribute a window share to a single block. The composition total is
// therefore the sum of its segment estimates — self-consistent by construction
// (cost.EstimateContextTokens over the same live blocks) — and an estimate for
// display, never a billing figure (billing always uses AS-020's reported
// counts).
//
// Selection: every Segment carries the stable schema.Block ID, so the segment
// list is the selection input the manual /clean view (AS-028) consumes. AS-026
// itself ships the read-only composition; the interactive multi-select UI lives
// with the removal it drives, in AS-028.
//
// The topic dimension (§7.11) depends on the labeling engine (AS-027, still in
// clarification) and is added when that lands; this view ships without it, with
// the type/file/recency/size dimensions that are mechanically derivable today.
package composition

import (
	"sort"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/memory"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// Tuning for the heuristic highlights. These shape display only — never the
// token total — so they can move without affecting the AC that segment tokens
// sum to the window total.
const (
	// topConsumerCount is how many largest segments the "top consumers" list
	// surfaces; the PRD AC is that the top 3 are identifiable in under 5s, so a
	// few extra rows give context without burying them.
	topConsumerCount = 5
	// staleTokenFloor and staleAge define a "stale candidate": a block large
	// enough to be worth reclaiming that has sat in the window a while. It is a
	// coarse heuristic flag (a reclaim hint), not a precise signal.
	staleTokenFloor = 800
	staleAge        = 5 * time.Minute
)

// Sort selects the order of the full segment list in the view.
type Sort int

const (
	// SortSize orders segments by token share, largest first (the default — it
	// puts the biggest consumers at the top, matching the PRD AC).
	SortSize Sort = iota
	// SortAge orders segments oldest first.
	SortAge
	// SortType groups segments by kind, largest group first, size-ordered within.
	SortType
)

// ParseSort maps a /context argument to a Sort, defaulting to SortSize for an
// empty or unrecognized value so the command degrades to its most useful view.
func ParseSort(s string) Sort {
	switch s {
	case "age", "recency", "old":
		return SortAge
	case "type", "kind", "group":
		return SortType
	default:
		return SortSize
	}
}

// Segment is one live block reduced to the dimensions the view ranks, groups
// and selects on. ID is the stable schema.Block ID — the key /clean (AS-028)
// removes by.
type Segment struct {
	ID      string
	Kind    schema.Kind
	Group   string        // display bucket: user / assistant / tool result / file read / reasoning / system+memory
	Origin  string        // file path, tool name, or role — where the block came from
	Tokens  int           // estimated window share (cost.EstimateBlockTokens)
	CostUSD float64       // Tokens priced at the active model's input rate
	Priced  bool          // false when the model is absent from the pricing table
	Age     time.Duration // now − append time
	Seq     int           // append order (recency)
	Path    string        // file path for a file read, else ""
	Live    bool          // in the model-facing window; false for an excluded segment
	Reason  string        // why a non-live segment was dropped (projection.Reason*)
}

// GroupTotal is the per-type rollup shown in the "by type" breakdown.
type GroupTotal struct {
	Group   string
	Tokens  int
	CostUSD float64
	Count   int
}

// Duplicate flags a file read more than once in the live window, with the
// combined cost of the repeats — a classic context waste the view surfaces.
type Duplicate struct {
	Path       string
	Count      int
	Tokens     int     // combined across the repeated reads
	CostUSD    float64 // combined
	Priced     bool
	SegmentIDs []string
}

// Composition is the full /context data model: the live window as ranked,
// grouped, and flagged segments plus the totals and highlights. It is the
// single value both the renderer and AS-028's selection UI read.
type Composition struct {
	Segments     []Segment    // live window segments, in the requested Sort order
	TotalTokens  int          // sum of segment tokens == cost.EstimateContextTokens(live)
	TotalCostUSD float64      // sum of segment costs (input-rate estimate)
	Priced       bool         // false when costs are a lower bound (model not priced)
	Currency     string       // money prefix, e.g. "$"
	Window       int          // model context-window size; 0 when unknown
	TopConsumers []Segment    // the largest segments, biggest first
	ByGroup      []GroupTotal // per-type rollup, largest group first
	Duplicates   []Duplicate  // files read more than once, biggest combined first
	Stale        []Segment    // large, old reclaim candidates, biggest first
	Excluded     []Segment    // blocks dropped from the window, biggest first (not in the total)
	Sort         Sort
}

// Build computes the composition of proj's live blocks, pricing each block's
// estimated tokens at model's input rate from table and dating it against now.
// It is pure: the same projection, table, model and now yield the same result,
// with no model calls — so /context opens instantly (AC) from log data alone.
func Build(proj *projection.Projection, table *cost.Table, model string, now time.Time, sortBy Sort) Composition {
	rate, priced := table.Lookup(model)
	window, _ := table.Window(model)

	c := Composition{
		Priced:   priced,
		Currency: cost.Symbol(table.Currency()),
		Window:   window,
		Sort:     sortBy,
	}

	groups := map[string]*GroupTotal{}
	var groupOrder []string
	dupes := map[string]*Duplicate{}
	var dupeOrder []string

	for _, b := range proj.Blocks() {
		tokens := cost.EstimateBlockTokens(b.Block)
		if tokens == 0 {
			continue // control or empty block: no window share to attribute
		}
		seg := segmentOf(b, tokens, rate, priced, now)

		// Excluded blocks are no longer in the window, so they are itemized in
		// their own section (the live/excluded dimension, AS-026) but kept out of
		// the window total and the consumer rankings — the total tracks the live
		// window, the AC that segment tokens sum to it.
		if !b.Live {
			c.Excluded = append(c.Excluded, seg)
			continue
		}
		c.Segments = append(c.Segments, seg)
		c.TotalTokens += tokens
		c.TotalCostUSD += seg.CostUSD

		g, ok := groups[seg.Group]
		if !ok {
			g = &GroupTotal{Group: seg.Group}
			groups[seg.Group] = g
			groupOrder = append(groupOrder, seg.Group)
		}
		g.Tokens += tokens
		g.CostUSD += seg.CostUSD
		g.Count++

		if seg.Path != "" {
			d, ok := dupes[seg.Path]
			if !ok {
				d = &Duplicate{Path: seg.Path, Priced: priced}
				dupes[seg.Path] = d
				dupeOrder = append(dupeOrder, seg.Path)
			}
			d.Count++
			d.Tokens += tokens
			d.CostUSD += seg.CostUSD
			d.SegmentIDs = append(d.SegmentIDs, seg.ID)
		}
	}

	c.ByGroup = collectGroups(groups, groupOrder)
	c.Duplicates = collectDuplicates(dupes, dupeOrder)
	c.TopConsumers = topConsumers(c.Segments)
	c.Stale = staleCandidates(c.Segments)
	sortSegments(c.Segments, sortBy)
	sort.SliceStable(c.Excluded, func(i, j int) bool { return c.Excluded[i].Tokens > c.Excluded[j].Tokens })
	return c
}

// segmentOf reduces a projected block to a Segment: its display group and
// origin, estimated token share priced at the input rate, recency, and its
// live/excluded status (the dimension /clean, AS-028, also reads).
func segmentOf(b projection.Block, tokens int, rate cost.Rate, priced bool, now time.Time) Segment {
	return Segment{
		ID:      b.ID,
		Kind:    b.Kind,
		Group:   groupFor(b.Block),
		Origin:  originFor(b.Block),
		Tokens:  tokens,
		CostUSD: priceTokens(tokens, rate, priced),
		Priced:  priced,
		Age:     now.Sub(b.TS),
		Seq:     b.Seq,
		Path:    filePath(b.Block),
		Live:    b.Live,
		Reason:  b.Reason,
	}
}

// priceTokens values tokens at a model's per-million input rate. Output/cache
// rates do not apply to a context-occupancy estimate: every live block is part
// of the next request's input regardless of how it was first produced.
func priceTokens(tokens int, rate cost.Rate, priced bool) float64 {
	if !priced {
		return 0
	}
	return float64(tokens) / 1e6 * rate.InputPerMTok
}

// groupFor buckets a block into the view's display group. Tool calls and tool
// results share the "tool" surface; file reads, reasoning, user and assistant
// text each get their own; system and harness blocks (memory, instructions)
// fold into "system+memory" per §7.11.
func groupFor(b schema.Block) string {
	// A skill's loaded instructions (AS-034) ride in on a tool_result but are
	// standing context, not a one-off tool answer; give them their own group so
	// /context shows skill cost distinctly from ordinary tool output.
	if b.Attribution != nil && b.Attribution.Skill != "" {
		return "skill"
	}
	switch b.Kind {
	case schema.KindFileRead:
		return "file read"
	case schema.KindReasoning:
		return "reasoning"
	case schema.KindToolCall, schema.KindToolResult:
		return "tool result"
	}
	switch b.Role {
	case schema.RoleUser:
		return "user"
	case schema.RoleAssistant:
		return "assistant"
	case schema.RoleSystem, schema.RoleHarness:
		return "system+memory"
	default:
		return string(b.Role)
	}
}

// originFor names where a block came from for the Origin column: the file path
// for a read, the tool name for a call/result, otherwise the role.
func originFor(b schema.Block) string {
	if path, ok := memory.Source(b); ok {
		return path // a memory file (AS-032): attribute the segment to its source
	}
	if b.Attribution != nil && b.Attribution.Skill != "" {
		return "skill: " + b.Attribution.Skill // a portable skill (AS-034)
	}
	switch {
	case b.FileRead != nil && b.FileRead.Path != "":
		return b.FileRead.Path
	case b.ToolCall != nil && b.ToolCall.Name != "":
		return b.ToolCall.Name
	case b.ToolResult != nil:
		if name := toolName(b); name != "" {
			return name
		}
		return "tool"
	default:
		return string(b.Role)
	}
}

// toolName prefers a tool result's attributed tool name, the precise label for
// the Origin column when present.
func toolName(b schema.Block) string {
	if b.Attribution != nil && b.Attribution.Tool != "" {
		return b.Attribution.Tool
	}
	return ""
}

// filePath returns the read path for a file-read block (the dedupe key), else "".
func filePath(b schema.Block) string {
	if b.FileRead != nil {
		return b.FileRead.Path
	}
	return ""
}

func collectGroups(groups map[string]*GroupTotal, order []string) []GroupTotal {
	out := make([]GroupTotal, 0, len(order))
	for _, name := range order {
		out = append(out, *groups[name])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	return out
}

func collectDuplicates(dupes map[string]*Duplicate, order []string) []Duplicate {
	var out []Duplicate
	for _, name := range order {
		if d := dupes[name]; d.Count >= 2 {
			out = append(out, *d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	return out
}

// topConsumers returns the largest segments, biggest first, capped at
// topConsumerCount. It works on a copy so it never reorders the caller's slice.
func topConsumers(segs []Segment) []Segment {
	ranked := append([]Segment(nil), segs...)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Tokens > ranked[j].Tokens })
	if len(ranked) > topConsumerCount {
		ranked = ranked[:topConsumerCount]
	}
	return ranked
}

// staleCandidates flags large, old segments as reclaim hints, biggest first.
func staleCandidates(segs []Segment) []Segment {
	var out []Segment
	for _, s := range segs {
		if s.Tokens >= staleTokenFloor && s.Age >= staleAge {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	return out
}

// sortSegments orders the full list in place per the requested Sort.
func sortSegments(segs []Segment, by Sort) {
	switch by {
	case SortAge:
		sort.SliceStable(segs, func(i, j int) bool { return segs[i].Seq < segs[j].Seq })
	case SortType:
		// Largest group first (matching the ByGroup rollup and the docs), then
		// largest segment within a group; the group name breaks an exact tie so the
		// order is deterministic.
		totals := map[string]int{}
		for _, s := range segs {
			totals[s.Group] += s.Tokens
		}
		sort.SliceStable(segs, func(i, j int) bool {
			if gi, gj := totals[segs[i].Group], totals[segs[j].Group]; gi != gj {
				return gi > gj
			}
			if segs[i].Group != segs[j].Group {
				return segs[i].Group < segs[j].Group
			}
			return segs[i].Tokens > segs[j].Tokens
		})
	default: // SortSize
		sort.SliceStable(segs, func(i, j int) bool { return segs[i].Tokens > segs[j].Tokens })
	}
}
