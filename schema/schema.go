// Package schema defines Agent Smith's immutable content-block schema (AS-003):
// the open, stable data substrate (PRD D1) over which every session is recorded
// as an append-only, immutable event log of content blocks (PRD D3).
//
// The schema is modeled as the union/superset of mainstream agent/provider wire
// formats (PRD D4); its shape is the one accepted by the AS-002 spike
// (docs/design/block-schema-union.md, §12). From V1 it is additive-only forever
// (PRD D2): no field is ever removed, renamed, or repurposed; consumers must
// tolerate missing and unknown fields. New concepts arrive only as new optional
// fields or new block kinds.
//
// Two escape hatches guarantee lossless re-emission of concepts the union does
// not yet model first-class: the Ext open maps (present on the envelope and on
// every sub-object) and Provider.NativeType / Provider.NativeID. A provider
// adapter that meets an unmodeled concept stores it explicitly there, so it
// survives a read -> store -> write cycle and can be promoted to a first-class
// optional field later with no breaking change.
package schema

import "encoding/json"

// SchemaVersion is the major version of the content-block schema. It is "1"
// from V1 onward; the additive-only discipline (PRD D2) means this does not
// change for additive evolution.
const SchemaVersion = "1"

// SchemaID identifies this schema in serialized documents.
const SchemaID = "agent-smith.blocks.v1"

// Document is a schema-version-tagged collection of blocks, used for
// serialization and round-trip testing. The canonical on-disk persistence
// format (append-only JSONL with exclusion / derived-block events) is owned by
// the event-log store (AS-005); Document is the minimal portable container for
// a set of blocks plus the schema tag.
type Document struct {
	Schema  string                     `json:"schema"`
	Version string                     `json:"schema_version"`
	Blocks  []Block                    `json:"blocks"`
	Ext     map[string]json.RawMessage `json:"ext,omitempty"`
}

// NewDocument returns a Document tagged with the current schema version. A nil
// blocks slice is normalized to an empty slice so the document marshals as
// "blocks": [] rather than "blocks": null, which is friendlier for consumers.
func NewDocument(blocks ...Block) Document {
	if blocks == nil {
		blocks = []Block{}
	}
	return Document{Schema: SchemaID, Version: SchemaVersion, Blocks: blocks}
}
