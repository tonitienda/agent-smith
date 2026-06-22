package eventlog

import (
	"encoding/json"
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

// KindModeEnter, KindPhaseChange, and KindModeExit are the Coding Mode (AS-072)
// lifecycle events: a control trio recording that the session entered an
// opinionated working mode, advanced through its phases, and exited again — so
// "current phase" is a projection over the log (PRD D3, D-CODE-3), never mutable
// side-state. They are non-content kinds carrying no content body beyond a label
// on Block.Text; a phase-change and an exit reference their mode instance through
// Provenance.DerivedFrom (the mode_enter block's ID is the instance ID).
//
// Like the other control kinds they live here rather than in the frozen
// content-block schema (AS-003) because they are harness/log control events, not
// content blocks; the schema tolerates non-content kinds (Block.Validate imposes
// no body constraint on them) and the projection engine (AS-006) never renders
// them into model-facing context. They are additive-only (PRD D2): a consumer
// that does not recognize them ignores them.
const (
	KindModeEnter   schema.Kind = "mode_enter"
	KindPhaseChange schema.Kind = "phase_change"
	KindModeExit    schema.Kind = "mode_exit"
)

// NewModeEnter builds a mode-enter event for modeName (e.g. "coding"), attributed
// to producer (e.g. "/mode"). The returned block's own ID is the mode instance ID
// that later phase-change and exit events reference; its Seq and append timestamp
// are assigned on append.
func NewModeEnter(producer, modeName string) schema.Block {
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       KindModeEnter,
		Role:       schema.RoleHarness,
		Text:       &schema.TextBody{Text: modeName, Subtype: schema.TextSubtypeNormal},
		Provenance: &schema.Provenance{Producer: producer},
	}
}

// NewPhaseChange builds a phase-change event moving the mode instance (instanceID,
// the mode_enter block's ID) to phase, attributed to producer (e.g. "/phase"). The
// instance is named on Provenance.DerivedFrom so the current phase is a projection
// over the log. The returned block has a fresh ID; its Seq and timestamp are
// assigned on append.
func NewPhaseChange(producer, instanceID, phase string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: KindPhaseChange,
		Role: schema.RoleHarness,
		Text: &schema.TextBody{Text: phase, Subtype: schema.TextSubtypeNormal},
		Provenance: &schema.Provenance{
			Producer:    producer,
			DerivedFrom: []string{instanceID},
		},
	}
}

// PhaseSkillProducer attributes the process-skill context blocks Coding Mode
// auto-loads per phase (AS-074), and ExtPhaseSkillPhase is the Block.Ext key under
// which each such block records (as a JSON string) the phase it was loaded for.
// They live here, beside the mode lifecycle kinds, because both the face that
// appends these blocks and the projection engine that scopes them to the active
// phase (AS-114) need the same vocabulary without depending on either other.
const (
	PhaseSkillProducer = "coding-mode/skills"
	ExtPhaseSkillPhase = "coding_mode_phase"
)

// NewModeExit builds a mode-exit event ending the mode instance (instanceID, the
// mode_enter block's ID), attributed to producer (e.g. "/mode"). The instance is
// named on Provenance.DerivedFrom; phase history stays on the log. The returned
// block has a fresh ID; its Seq and timestamp are assigned on append.
func NewModeExit(producer, instanceID string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: KindModeExit,
		Role: schema.RoleHarness,
		Provenance: &schema.Provenance{
			Producer:    producer,
			DerivedFrom: []string{instanceID},
		},
	}
}

// KindEscalation is the event kind for a routing auto-escalation event: a control
// event recording that a tier-declared, model-using task retried on a stronger
// tier after a structured low-confidence/failed attempt (AS-110 primitive, AS-116
// visibility, PRD §7.15). It is a non-content kind carrying no content body — the
// escalation record (feature, the tiers it moved between, and the producer's
// structured reason) rides on Block.Ext under escalationExtKey, and the producer
// on Provenance. /route reads these events to show that an escalation occurred,
// and the retry's own usage event attributes its extra spend in /cost.
//
// Like the other control kinds it lives here rather than in the frozen content-
// block schema (AS-003) because it is a harness/log control event, not a content
// block, and it uses the schema's additive Ext escape hatch (D2) rather than a
// new envelope field so it never touches the frozen union. The projection engine
// (AS-006) never renders it into model-facing context.
const KindEscalation schema.Kind = "escalation"

// escalationExtKey is the Block.Ext key the escalation payload is stored under.
const escalationExtKey = "escalation"

// Escalation is the decoded payload of a KindEscalation event: which feature
// escalated, the tiers it moved between (as their string names), and the
// structured reason the producer reported (never invented — §9 mitigation).
type Escalation struct {
	Feature string `json:"feature"`
	From    string `json:"from"`
	To      string `json:"to"`
	Reason  string `json:"reason"`
}

// NewEscalation builds an escalation event attributed to producer (the escalating
// feature, e.g. the /compact summarizer), recording feature, the from→to tiers,
// and the structured reason. The payload rides on Ext so the frozen schema is
// untouched (D2 additive-only).
//
// The returned block has a fresh ID; its Seq and append timestamp are assigned
// when it is appended to a Log.
func NewEscalation(producer string, esc Escalation) schema.Block {
	payload, _ := json.Marshal(esc) //nolint:errcheck // a fixed-shape struct never fails to marshal
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       KindEscalation,
		Role:       schema.RoleHarness,
		Provenance: &schema.Provenance{Producer: producer},
		Ext:        map[string]json.RawMessage{escalationExtKey: payload},
	}
}

// EscalationOf decodes b as an escalation event, reporting false for any other
// kind or a payload that does not parse (defensive, mirroring BudgetLimit's
// tolerate-and-skip posture).
func EscalationOf(b schema.Block) (Escalation, bool) {
	if b.Kind != KindEscalation {
		return Escalation{}, false
	}
	raw, ok := b.Ext[escalationExtKey]
	if !ok {
		return Escalation{}, false
	}
	var esc Escalation
	if err := json.Unmarshal(raw, &esc); err != nil {
		return Escalation{}, false
	}
	return esc, true
}

// BudgetLimit returns the most recently set session ceiling on the log and
// whether any budget event exists, scanning events in append order so the last
// /budget wins. A budget event whose Text does not parse as a number is skipped
// defensively rather than aborting accounting.
func BudgetLimit(events []schema.Block) (limitUSD float64, ok bool) {
	// Scan in reverse so the most recently set ceiling is found first — O(1) in the
	// common case that a budget was set recently, mirroring lastModel.
	for i := len(events) - 1; i >= 0; i-- {
		b := events[i]
		if b.Kind != KindBudget || b.Text == nil {
			continue
		}
		v, err := strconv.ParseFloat(b.Text.Text, 64)
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
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
