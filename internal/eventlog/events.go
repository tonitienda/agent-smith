package eventlog

import (
	"time"

	"github.com/tonitienda/agent-smith/schema"
)

// KindExclusion is the event kind for an exclusion event: a control event that
// drops one or more blocks from the model-facing projection without mutating or
// removing them from the log (PRD D3). It is a non-content kind, so it carries
// no content body — the blocks it removes are named in Provenance.DerivedFrom,
// and the projection engine (AS-006) folds those IDs into the excluded set.
//
// It is defined here rather than in the frozen content-block schema (AS-003)
// because it is a harness/log control event, not a content block; the schema
// tolerates non-content kinds by design (Block.Validate imposes no body
// constraint on them).
const KindExclusion schema.Kind = "exclusion"

// NewExclusion builds an exclusion event that removes the given block IDs from
// the projection, attributed to producer (the command or agent that created it,
// e.g. "/clean"). The event is immutable history like any other: undoing it is a
// further appended exclusion-counter event, never a deletion.
//
// The returned block has a fresh ID; its Seq and append timestamp are assigned
// when it is appended to a Log.
func NewExclusion(producer string, removes ...string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: KindExclusion,
		Role: schema.RoleHarness,
		Provenance: &schema.Provenance{
			Producer:    producer,
			DerivedFrom: append([]string(nil), removes...),
		},
	}
}

// Derive stamps a caller-built replacement block as a derived-block event:
// derived from the given source block IDs and attributed to producer. The
// derived block replaces its sources in the projection — the sources are
// excluded (via the same Provenance.DerivedFrom mechanism as an exclusion)
// while the replacement's own content appears, with provenance preserved so the
// edit is reversible and auditable (PRD D3).
//
// b is expected to carry a derived kind (e.g. schema.KindCompaction) and its
// replacement body. Existing Provenance fields on b are preserved; only
// Producer and DerivedFrom are set/extended.
func Derive(b schema.Block, producer string, sources ...string) schema.Block {
	// b is a value copy, but b.Provenance is a pointer shared with the caller's
	// block. Copy the Provenance struct and its DerivedFrom slice so stamping the
	// derived block never mutates the source block the caller still holds.
	var prov schema.Provenance
	if b.Provenance != nil {
		prov = *b.Provenance
	}
	prov.Producer = producer
	prov.DerivedFrom = append(append([]string(nil), prov.DerivedFrom...), sources...)
	b.Provenance = &prov

	if b.ID == "" {
		b.ID = schema.NewID()
	}
	if b.TS.IsZero() {
		b.TS = time.Now().UTC()
	}
	return b
}
