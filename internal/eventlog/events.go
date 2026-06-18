package eventlog

import (
	"strconv"
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

// KindUsage is the event kind for a usage event: a control event recording one
// provider turn's token usage and price-affecting metadata so token/cost
// accounting (AS-020) is derivable from the log without re-querying the
// provider. It is a non-content kind carrying no content body — the counts live
// on the envelope (Block.Tokens, Block.UsageMeta), the model that served the
// turn on Block.Provider, and the turn's stop reason on Block.StopReason.
//
// Like KindExclusion it is defined here rather than in the frozen content-block
// schema (AS-003) because it is a harness/log control event, not a content
// block; the schema tolerates non-content kinds (Block.Validate imposes no body
// constraint on them) and the projection engine (AS-006) never renders it into
// model-facing context.
const KindUsage schema.Kind = "usage"

// NewUsage builds a usage event recording the tokens and metadata a single
// provider turn reported, attributed to producer (the loop). vendor and model
// identify the surface that served the turn so accounting can price it;
// stopReason carries the turn's normalized stop reason. tokens and meta may be
// nil when the surface reported nothing.
//
// The returned block has a fresh ID; its Seq and append timestamp are assigned
// when it is appended to a Log.
func NewUsage(producer, vendor, model, stopReason string, tokens *schema.Tokens, meta *schema.UsageMeta) schema.Block {
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       KindUsage,
		Role:       schema.RoleHarness,
		StopReason: stopReason,
		Tokens:     tokens,
		UsageMeta:  meta,
		Provider:   &schema.Provider{Vendor: vendor, Model: model},
		Provenance: &schema.Provenance{Producer: producer},
	}
}

// KindModelSwitch is the event kind for a model-switch event: a control event
// recording that the user changed the active provider/model mid-session (AS-023
// /model), so cost attribution and the transcript stay accurate and a resumed
// session can recover the model it was last using. It is a non-content kind
// carrying no content body — the switched-to surface lives on Block.Provider and
// the command that made the switch on Provenance.Producer.
//
// Like KindExclusion and KindUsage it lives here rather than in the frozen
// content-block schema (AS-003) because it is a harness/log control event, not a
// content block; the schema tolerates non-content kinds (Block.Validate imposes
// no body constraint on them) and the projection engine (AS-006) never renders
// it into model-facing context.
const KindModelSwitch schema.Kind = "model_switch"

// NewModelSwitch builds a model-switch event attributed to producer (the command
// that switched, e.g. "/model"), recording the vendor and model now in effect.
//
// The returned block has a fresh ID; its Seq and append timestamp are assigned
// when it is appended to a Log.
func NewModelSwitch(producer, vendor, model string) schema.Block {
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       KindModelSwitch,
		Role:       schema.RoleHarness,
		Provider:   &schema.Provider{Vendor: vendor, Model: model},
		Provenance: &schema.Provenance{Producer: producer},
	}
}

// KindSkillLoad is the event kind for a skill-load event: a control event
// recording that a portable skill (AS-034) was available to the model this
// session, so the living-skills analyzers (AS-047/048/049) have a stable hook
// point for "what skills were loaded" without re-scanning the filesystem. It is
// a non-content kind carrying no content body — the skill name lives on
// Attribution.Skill and the producer ("skill-loader") on Provenance.
//
// Like the other control kinds above it lives here rather than in the frozen
// content-block schema (AS-003) because it is a harness/log control event, not a
// content block; the schema tolerates non-content kinds (Block.Validate imposes
// no body constraint on them) and the projection engine (AS-006) never renders
// it into model-facing context. A skill's actual instructions enter the context
// (and so /context) only when the model invokes the skill — that activation is
// recorded as the skill tool's tool_result, attributed to the skill.
const KindSkillLoad schema.Kind = "skill_load"

// NewSkillLoad builds a skill-load event for the named skill, attributed to
// producer (the loader). The returned block has a fresh ID; its Seq and append
// timestamp are assigned when it is appended to a Log.
func NewSkillLoad(producer, name string) schema.Block {
	return schema.Block{
		ID:          schema.NewID(),
		Kind:        KindSkillLoad,
		Role:        schema.RoleHarness,
		Provenance:  &schema.Provenance{Producer: producer},
		Attribution: &schema.Attribution{Skill: name},
	}
}

// KindHookNote is the event kind for a hook note: a control event recording that
// a lifecycle hook (AS-035) annotated the session — e.g. a post-tool-use hook
// leaving a note, or a session-start hook recording context. It is a non-content
// kind carrying its note text on Block.Text and the originating event name on
// Attribution.Tool (reused as a free-form label), so the note is auditable on the
// log without entering model-facing context.
//
// Like the other control kinds it lives here rather than in the frozen
// content-block schema (AS-003): the schema tolerates non-content kinds and the
// projection engine (AS-006) never renders it into the window. A hook that wants
// the model to *see* something blocks or modifies instead; an annotation is a
// record, not an injection.
const KindHookNote schema.Kind = "hook_note"

// NewHookNote builds a hook-note event carrying note as text, labeled with the
// lifecycle event that produced it and attributed to producer. The returned
// block has a fresh ID; its Seq and append timestamp are assigned on append.
func NewHookNote(producer, event, note string) schema.Block {
	return schema.Block{
		ID:          schema.NewID(),
		Kind:        KindHookNote,
		Role:        schema.RoleHarness,
		Text:        &schema.TextBody{Text: note, Subtype: schema.TextSubtypeNormal},
		Provenance:  &schema.Provenance{Producer: producer},
		Attribution: &schema.Attribution{Tool: event},
	}
}

// KindCheckpoint is the event kind for a manual rewind checkpoint (AS-037
// `/rewind --mark "<label>"`): a control event marking a point the conversation
// can later be rewound to, carrying the user's label on Block.Text. Automatic
// checkpoints are derived from user turns and need no event; only named manual
// marks are recorded, so the rewind picker can offer them.
//
// Like the other control kinds it lives here rather than in the frozen
// content-block schema (AS-003) because it is a harness/log control event, not a
// content block; the schema tolerates non-content kinds (Block.Validate imposes
// no body constraint on them) and the projection engine (AS-006) never renders
// it into model-facing context.
const KindCheckpoint schema.Kind = "checkpoint"

// NewCheckpoint builds a manual checkpoint event labeled with label, attributed
// to producer (e.g. "/rewind --mark"). The returned block has a fresh ID; its
// Seq and append timestamp are assigned when it is appended to a Log.
func NewCheckpoint(producer, label string) schema.Block {
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       KindCheckpoint,
		Role:       schema.RoleHarness,
		Text:       &schema.TextBody{Text: label, Subtype: schema.TextSubtypeNormal},
		Provenance: &schema.Provenance{Producer: producer},
	}
}

// KindBudget is the event kind for a budget-ceiling event: a control event
// recording that the user set (or cleared) the session's spend ceiling in
// dollars (AS-041 /budget), so budget enforcement is derivable from the log and
// survives save/resume — the latest budget event is the active ceiling. It is a
// non-content kind carrying its ceiling on Block.Text (a decimal dollar string);
// a ceiling of "0" clears the budget.
//
// Like the other control kinds it lives here rather than in the frozen
// content-block schema (AS-003) because it is a harness/log control event, not a
// content block; the schema tolerates non-content kinds (Block.Validate imposes
// no body constraint on them) and the projection engine (AS-006) never renders
// it into model-facing context.
const KindBudget schema.Kind = "budget"

// NewBudget builds a budget-ceiling event recording limitUSD, attributed to
// producer (e.g. "/budget"). The ceiling is stored as a plain decimal string on
// Block.Text so it round-trips exactly through the JSONL log. The returned block
// has a fresh ID; its Seq and append timestamp are assigned on append.
func NewBudget(producer string, limitUSD float64) schema.Block {
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       KindBudget,
		Role:       schema.RoleHarness,
		Text:       &schema.TextBody{Text: strconv.FormatFloat(limitUSD, 'f', -1, 64), Subtype: schema.TextSubtypeNormal},
		Provenance: &schema.Provenance{Producer: producer},
	}
}

// BudgetLimit returns the most recently set session ceiling on the log and
// whether any budget event exists, scanning events in append order so the last
// /budget wins. A budget event whose Text does not parse as a number is skipped
// defensively rather than aborting accounting.
func BudgetLimit(events []schema.Block) (limitUSD float64, ok bool) {
	for _, b := range events {
		if b.Kind != KindBudget || b.Text == nil {
			continue
		}
		v, err := strconv.ParseFloat(b.Text.Text, 64)
		if err != nil {
			continue
		}
		limitUSD, ok = v, true
	}
	return limitUSD, ok
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
