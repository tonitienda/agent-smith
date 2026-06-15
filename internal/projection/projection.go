// Package projection computes the model-facing context as a pure projection
// over the append-only event log (AS-006, PRD D3). The context the model sees
// is never stored state: it is recomputed from the log on demand, so the same
// log always yields the same projection.
//
// The log records edits as appended control events, never mutations (eventlog):
//
//   - An exclusion event (eventlog.KindExclusion) names blocks in
//     Provenance.DerivedFrom; while it is in effect, those blocks leave the
//     projection but remain on the log and in any archive.
//   - A derived block (e.g. schema.KindCompaction via eventlog.Derive) both
//     renders its own replacement content and excludes the source blocks it was
//     computed from — the same DerivedFrom mechanism — with provenance
//     preserved so the edit is reversible and auditable.
//
// Because edits are themselves events, an edit is undone by appending another
// event that excludes the editing event: an exclusion whose target is an
// earlier exclusion (or derived block) nullifies it, restoring the projection
// exactly. This composes to any depth and is resolved by a single reverse pass
// (see project).
//
// Point-in-time projection (ProjectAt) projects the log as of event n by
// considering only the first n events, which is the structural basis for
// /rewind: a later counter-event simply does not exist yet at that point.
package projection

import (
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// Options controls how a projection is computed. The zero value is a valid,
// fully deterministic, model-agnostic projection: every rendered block that is
// not excluded by an active event is live.
type Options struct {
	// TargetModel, when non-empty, enables reasoning-replay filtering. A
	// reasoning block whose ReplayScope is schema.ReplaySameModelOnly may only
	// be replayed to the model that produced it (Anthropic thinking blocks): it
	// is dropped from the projection unless its producing model (Provider.Model)
	// equals TargetModel. Portable reasoning (the default when ReplayScope is
	// unset) is always kept.
	//
	// Empty disables the filter so every reasoning block projects, keeping the
	// default deterministic and independent of any provider.
	TargetModel string
}

// Reasons a rendered block is not live in the projection.
const (
	// ReasonExcluded: dropped by one or more active exclusion / derived-block
	// events; their IDs are in Block.ExcludedBy.
	ReasonExcluded = "excluded"
	// ReasonReplayScope: a same-model-only reasoning block dropped because the
	// target model differs from the model that produced it (see Options).
	ReasonReplayScope = "replay_scope"
)

// Block is a projected block: a copy of the log event plus projection-time
// metadata. The embedded schema.Block carries type, origin, token counts and
// recency (Seq) for downstream readers (/context, /clean, the cost meter); the
// copy is independent of the log, though its pointer fields are shared and must
// be treated as read-only.
//
// On an excluded block, the embedded ExcludedBy field is populated with the IDs
// of the active events that dropped it — derived metadata that is empty on the
// stored log event.
type Block struct {
	schema.Block

	// Live reports whether the block is part of the model-facing context.
	Live bool `json:"live"`
	// Reason is "" when Live, otherwise why it was dropped (ReasonExcluded or
	// ReasonReplayScope).
	Reason string `json:"reason,omitempty"`
}

// Projection is the computed view over a slice of log events: every rendered
// block in append order, each tagged live or dropped. Control-only events
// (pure exclusions) are the mechanism, not content, so they do not appear here.
type Projection struct {
	blocks  []Block
	liveLen int // count of live blocks, tallied during ProjectAt
}

// Project computes the projection over every event in order. events is
// typically eventlog.Log.Events(); it is treated as read-only and never
// retained.
func Project(events []schema.Block, opts Options) *Projection {
	return ProjectAt(events, len(events), opts)
}

// ProjectAt computes the projection as of event n: only events[:n] are
// considered, so any exclusion or derived-block event at index >= n has no
// effect. n is clamped to [0, len(events)]. This is the point-in-time
// projection that /rewind builds on.
func ProjectAt(events []schema.Block, n int, opts Options) *Projection {
	if n < 0 {
		n = 0
	}
	if n > len(events) {
		n = len(events)
	}
	events = events[:n]

	excludedBy := computeExclusions(events)

	// At most one rendered block per event; pre-size to avoid reallocations.
	p := &Projection{blocks: make([]Block, 0, len(events))}
	for _, e := range events {
		if !isRendered(e) {
			continue
		}
		pb := Block{Block: e}
		if ids := excludedBy[e.ID]; len(ids) > 0 {
			pb.Live = false
			pb.Reason = ReasonExcluded
			pb.ExcludedBy = ids
		} else if droppedByReplay(e, opts) {
			pb.Live = false
			pb.Reason = ReasonReplayScope
		} else {
			pb.Live = true
			p.liveLen++
		}
		p.blocks = append(p.blocks, pb)
	}
	return p
}

// computeExclusions returns, for each excluded block ID, the IDs of the active
// events that exclude it.
//
// An event is a control event if it names sources in Provenance.DerivedFrom
// (an exclusion, or a derived block that replaces its sources). A control event
// is active unless it is itself excluded by a later active control event —
// which is how an undo (an exclusion targeting an earlier exclusion) cancels
// the original.
//
// DerivedFrom references only earlier events, so a single reverse pass settles
// activeness: when an event is reached, every event that could exclude it
// (higher Seq) has already been processed and recorded its effect.
func computeExclusions(events []schema.Block) map[string][]string {
	// excludedIDs[id] = IDs of active events that exclude block id, in the order
	// processed (reverse). Membership also marks an event as inactive.
	excludedIDs := make(map[string][]string)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		active := len(excludedIDs[e.ID]) == 0
		if !active {
			continue
		}
		if e.Provenance == nil {
			continue
		}
		for _, target := range e.Provenance.DerivedFrom {
			excludedIDs[target] = append(excludedIDs[target], e.ID)
		}
	}
	// The reverse pass collects excluding IDs in descending-Seq order; reverse
	// each to the natural append order so the result is deterministic and
	// reads chronologically.
	for id, by := range excludedIDs {
		for l, r := 0, len(by)-1; l < r; l, r = l+1, r-1 {
			by[l], by[r] = by[r], by[l]
		}
		excludedIDs[id] = by
	}
	return excludedIDs
}

// isRendered reports whether an event contributes content to the projection.
// Control-only events — exclusions (eventlog.KindExclusion) and usage/accounting
// records (eventlog.KindUsage) — are never rendered; every content kind and
// derived block (compaction, fallback) is.
func isRendered(b schema.Block) bool {
	return b.Kind != eventlog.KindExclusion && b.Kind != eventlog.KindUsage
}

// droppedByReplay reports whether a same-model-only reasoning block must be
// dropped because the target model differs from the model that produced it.
func droppedByReplay(b schema.Block, opts Options) bool {
	if opts.TargetModel == "" || b.Kind != schema.KindReasoning || b.Reasoning == nil {
		return false
	}
	if b.Reasoning.ReplayScope != schema.ReplaySameModelOnly {
		return false
	}
	producing := ""
	if b.Provider != nil {
		producing = b.Provider.Model
	}
	return producing != opts.TargetModel
}

// Live returns the ordered, model-facing context: a copy of each live block in
// append order, ready for provider request assembly. The slice is freshly
// allocated; the blocks share the log's pointer fields and are read-only.
//
// Prefix-stability invariant (AS-011): live blocks are emitted in append order
// and blocks are immutable once logged, so for two projections of the same
// growing session the live sequence of one is a prefix of the other up to the
// first divergence. An exclusion drops a block but never reorders or mutates the
// blocks before it, so everything ahead of the first changed block is byte-for-byte
// identical across turns. This is what makes provider prompt caching pay off:
// adapters serialize this order deterministically, so an unchanged prefix stays
// byte-identical turn to turn and keeps hitting the cache, and a mid-session edit
// only invalidates the cache from the first changed block onward — never the
// whole prefix. Anything that consumes Live() for a cached request must preserve
// that ordering and serialize deterministically.
func (p *Projection) Live() []schema.Block {
	out := make([]schema.Block, 0, p.liveLen)
	for _, b := range p.blocks {
		if b.Live {
			out = append(out, b.Block)
		}
	}
	return out
}

// Blocks returns every rendered block in append order — live and dropped —
// each carrying its live/excluded status and metadata. This is the view
// /context and /clean read. The returned slice is freshly allocated.
func (p *Projection) Blocks() []Block {
	out := make([]Block, len(p.blocks))
	copy(out, p.blocks)
	return out
}

// Len returns the number of rendered blocks (live plus dropped).
func (p *Projection) Len() int { return len(p.blocks) }

// LiveLen returns the number of live blocks in the model-facing context.
func (p *Projection) LiveLen() int { return p.liveLen }
