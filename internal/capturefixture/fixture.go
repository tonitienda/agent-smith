// Package capturefixture implements the capture-to-fixture workflow (AS-135): it
// turns a redacted, reviewed vendor/CLI session capture (a JSONL of schema.Block
// — the format AS-060 produces) into a deterministic, CI-safe fixture that the
// recorded vendor simulators (AS-133) and the offline E2E suite (AS-134) replay.
//
// Two transforms run over every captured block, in order:
//
//   - Normalize — identifying and non-deterministic envelope values (block IDs,
//     sequence, timestamps, request/response/turn IDs, provider native IDs,
//     sub-agent thread/agent IDs, and the block-ID references in derived_from /
//     excluded_by / thread parents) are replaced with stable, deterministic
//     placeholders. References are mapped through a single per-namespace table so
//     referential integrity (a thread's parent, a derived block's source) is
//     preserved while the original values are gone.
//   - Redact — the block body is scrubbed through internal/redaction (the AS-115
//     rules) so a high-confidence secret that slipped into the capture never
//     reaches the committed fixture.
//
// Neither transform changes a block's kind, body shape, streaming structure, tool
// arguments/results, usage, cache semantics, or sub-agent/session links — only
// the *values* that identify a real account or leak a secret — so the fixture
// still exercises every schema field the simulator must reproduce.
//
// The package is library-first (a thin CLI lives in cmd/capture-fixture) so the
// workflow is feature-testable offline with no network and no vendor keys.
package capturefixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tonitienda/agent-smith/internal/redaction"
	"github.com/tonitienda/agent-smith/schema"
)

// fixtureEpoch is the deterministic base timestamp normalized captures count
// from: block i is stamped fixtureEpoch + i seconds. A fixed epoch keeps the
// fixture byte-stable across runs and machines while preserving block ordering.
var fixtureEpoch = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

// Stats summarizes what Process changed, for the validation report and the
// generated fixture metadata. It records nothing about the secret values
// themselves — only counts — so it is safe to commit.
type Stats struct {
	Blocks         int            `json:"blocks"`
	RedactionSpans int            `json:"redaction_spans"`
	Normalized     map[string]int `json:"normalized"` // distinct values replaced, per namespace
}

// Process normalizes and redacts a captured session into a CI-safe fixture. It
// returns the transformed blocks, summary stats, and any per-block schema
// validation errors (a malformed capture is reported, not silently emitted). The
// input slice is not mutated. A nil redactor skips redaction (normalize only);
// pass redaction.Default() for the standard scrub.
func Process(blocks []schema.Block, r *redaction.Redactor) ([]schema.Block, Stats, []error) {
	n := newNormalizer()
	out := make([]schema.Block, 0, len(blocks))
	var spans int
	var verr []error
	for i, b := range blocks {
		b = n.block(b, i)
		if r != nil {
			if rb, changed := r.Redact(b); changed {
				b = rb
				spans += redactionSpans(rb)
			}
		}
		if err := b.Validate(); err != nil {
			verr = append(verr, fmt.Errorf("block %d (seq %d): %w", i, b.Seq, err))
		}
		out = append(out, b)
	}
	return out, Stats{Blocks: len(out), RedactionSpans: spans, Normalized: n.counts()}, verr
}

// redactionSpans reads back the Total from the structural redaction record the
// redactor stamps into Ext, so the report can show how much was scrubbed without
// the redactor exposing a count directly.
func redactionSpans(b schema.Block) int {
	raw, ok := b.Ext[redaction.ExtKey]
	if !ok {
		return 0
	}
	var rec redaction.Record
	if json.Unmarshal(raw, &rec) != nil {
		return 0
	}
	return rec.Total
}

// normalizer assigns each distinct original identifier a stable placeholder,
// keyed by namespace so values that mean different things never collide and
// references to the same value always map alike (referential integrity).
type normalizer struct {
	maps map[string]map[string]string // namespace -> original -> placeholder
	seq  map[string]int               // namespace -> next ordinal
}

func newNormalizer() *normalizer {
	return &normalizer{maps: map[string]map[string]string{}, seq: map[string]int{}}
}

// id returns the stable placeholder for orig within ns, minting a new one on
// first sight. An empty original maps to empty (an absent field stays absent).
func (n *normalizer) id(ns, orig string) string {
	if orig == "" {
		return ""
	}
	m := n.maps[ns]
	if m == nil {
		m = map[string]string{}
		n.maps[ns] = m
	}
	if v, ok := m[orig]; ok {
		return v
	}
	n.seq[ns]++
	v := fmt.Sprintf("%s-%04d", ns, n.seq[ns])
	m[orig] = v
	return v
}

// ids maps a slice of references through the same namespace, preserving order.
func (n *normalizer) ids(ns string, orig []string) []string {
	if len(orig) == 0 {
		return orig
	}
	out := make([]string, len(orig))
	for i, s := range orig {
		out[i] = n.id(ns, s)
	}
	return out
}

// counts reports how many distinct values were replaced per namespace.
func (n *normalizer) counts() map[string]int {
	c := make(map[string]int, len(n.maps))
	for ns, m := range n.maps {
		c[ns] = len(m)
	}
	return c
}

// block returns a copy of b with its identifying envelope fields normalized. The
// "blk" namespace is shared by the block's own ID and every reference to a block
// ID (derived_from, excluded_by, thread parent), so cross-block links survive the
// rewrite. i drives the deterministic seq and timestamp.
func (n *normalizer) block(b schema.Block, i int) schema.Block {
	b.ID = n.id("blk", b.ID)
	b.Seq = i
	b.TS = fixtureEpoch.Add(time.Duration(i) * time.Second)
	b.ExcludedBy = n.ids("blk", b.ExcludedBy)

	if b.Provenance != nil {
		p := *b.Provenance
		p.RequestID = n.id("req", p.RequestID)
		p.ResponseID = n.id("resp", p.ResponseID)
		p.TurnID = n.id("turn", p.TurnID)
		p.DerivedFrom = n.ids("blk", p.DerivedFrom)
		b.Provenance = &p
	}
	if b.Provider != nil {
		pv := *b.Provider
		pv.NativeID = n.id("native", pv.NativeID)
		b.Provider = &pv
	}
	if b.Thread != nil {
		t := *b.Thread
		t.ThreadID = n.id("thread", t.ThreadID)
		t.ParentThreadID = n.id("thread", t.ParentThreadID)
		t.ParentBlockID = n.id("blk", t.ParentBlockID)
		t.AgentID = n.id("agent", t.AgentID)
		b.Thread = &t
	}
	// Tool-use IDs share the "toolu" namespace so a file read's produced_by maps to
	// the same placeholder as the tool call that produced it (referential integrity).
	if b.ToolCall != nil {
		tc := *b.ToolCall
		tc.ToolUseID = n.id("toolu", tc.ToolUseID)
		b.ToolCall = &tc
	}
	if b.ToolResult != nil {
		tr := *b.ToolResult
		tr.ToolUseID = n.id("toolu", tr.ToolUseID)
		b.ToolResult = &tr
	}
	if b.FileRead != nil {
		fr := *b.FileRead
		fr.ProducedBy = n.id("toolu", fr.ProducedBy)
		b.FileRead = &fr
	}
	// Cache breakpoints reference block IDs — remap through the "blk" namespace so
	// the breakpoint still points at the normalized block.
	if b.Cache != nil && len(b.Cache.Breakpoints) > 0 {
		c := *b.Cache
		bps := make([]schema.CacheBreakpoint, len(c.Breakpoints))
		for i, bp := range c.Breakpoints {
			bp.BlockID = n.id("blk", bp.BlockID)
			bps[i] = bp
		}
		c.Breakpoints = bps
		b.Cache = &c
	}
	return b
}

// Allowed metadata enum values. Source is the data's provenance; RedactionStatus
// is its lifecycle state — the four-way distinction the workflow keeps explicit
// (raw private capture vs redacted capture vs synthetic derivative vs public CI
// fixture).
var (
	allowedSources  = []string{"real-capture", "synthetic", "hand-authored"}
	allowedStatuses = []string{"raw-private", "redacted", "synthetic-derivative", "public-ci"}
)

// Metadata is the sidecar record committed next to a fixture. It tells AS-133
// where the fixture came from, whether it was redacted, what adapter behavior it
// guards, which provider shapes it covers, and whether a live-network capture can
// reproduce it. Stats is filled by Process; the rest is supplied by the
// contributor.
type Metadata struct {
	Source           string   `json:"source"`            // real-capture | synthetic | hand-authored
	RedactionStatus  string   `json:"redaction_status"`  // raw-private | redacted | synthetic-derivative | public-ci
	Intent           string   `json:"intent"`            // adapter behavior this fixture guards
	Providers        []string `json:"providers"`         // vendor/surface shapes covered
	LiveReproducible bool     `json:"live_reproducible"` // can a live API call recreate it
	Stats            Stats    `json:"stats"`
}

// Validate checks the contributor-supplied metadata so a fixture is never
// committed with an unknown source/status or an empty intent.
func (m Metadata) Validate() error {
	if !contains(allowedSources, m.Source) {
		return fmt.Errorf("metadata: source %q not one of %v", m.Source, allowedSources)
	}
	if !contains(allowedStatuses, m.RedactionStatus) {
		return fmt.Errorf("metadata: redaction_status %q not one of %v", m.RedactionStatus, allowedStatuses)
	}
	if m.Intent == "" {
		return fmt.Errorf("metadata: intent is required (what adapter behavior does this fixture guard?)")
	}
	if len(m.Providers) == 0 {
		return fmt.Errorf("metadata: at least one provider shape is required")
	}
	return nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// Marshal renders the metadata as stable, indented JSON (providers sorted) for a
// byte-reproducible sidecar file. HTML escaping is off so a `>` in the intent
// stays readable rather than becoming `>`.
func (m Metadata) Marshal() ([]byte, error) {
	sort.Strings(m.Providers)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
