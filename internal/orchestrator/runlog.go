package orchestrator

// AS-151 — Smith event-log integration for orchestrated runs.
//
// The ADR (D-ORCH-4) splits orchestration state in two: the SQLite run store
// (AS-161) holds run-control rows (jobs/triggers/runs/leases/attempts/idempotency
// /audit) and nothing narrative, while "each run is a normal append-only Smith
// session" so /context, /cost, /insights, and replay reuse the existing readers
// with no second observability path. This file is that session seam: a Recorder
// creates one ordinary Smith session per run, stamps its identity linkage onto
// the session metadata, and appends the run's lifecycle — policy decisions,
// GitHub actions, provider usage, referenced artifacts, and the terminal outcome
// — as event-log blocks a cost/insights reader already understands.
//
// The orchestration blocks are non-content harness kinds carrying their payload
// on Block.Ext (the D2 additive escape hatch, exactly as KindEscalation does),
// so the frozen content-block union (AS-003) is untouched and the projection
// engine (AS-006) never renders them into model-facing context. Provider spend
// is recorded as ordinary eventlog.KindUsage blocks, so cost.Summarize prices an
// orchestrated session with no special case (acceptance: "Cost and insights
// readers can process orchestrated sessions without a separate code path").

import (
	"encoding/json"
	"fmt"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/schema"
)

// producer attributes every orchestration block to the orchestrator shell so the
// transcript distinguishes deterministic-shell writes from a step's own output.
const producer = "orchestrator"

// Orchestration event kinds. Each is a harness control kind (not a frozen content
// kind), so Block.Validate imposes no body constraint and the payload rides on
// Block.Ext under the matching key.
const (
	// KindRunStart opens an orchestrated run's session with its identity linkage.
	KindRunStart schema.Kind = "orchestration_run_start"
	// KindPolicyDecision records one policy check's verdict (merge gate, permission
	// check, budget gate) so the audit trail lives in the readable session, not
	// only the run store.
	KindPolicyDecision schema.Kind = "orchestration_policy"
	// KindGitHubAction records one deterministic GitHub side effect (branch, PR,
	// comment, status) with its refs and links.
	KindGitHubAction schema.Kind = "orchestration_github"
	// KindArtifactRef references a large run artifact by integrity hash and URI
	// rather than embedding its bytes in the JSONL log.
	KindArtifactRef schema.Kind = "orchestration_artifact"
	// KindRunOutcome closes the run with its terminal status, failure class, and
	// cost.
	KindRunOutcome schema.Kind = "orchestration_run_outcome"
)

// extKey is the single Block.Ext key each orchestration kind stores its payload
// under; every kind uses the same key so decoding is uniform.
const extKey = "orchestration"

// linkExtKey is the Metadata.Ext key the run linkage is stored under so operator
// tooling can list runs from metadata.json without replaying the event log.
const linkExtKey = "orchestration"

// RunLink is the identity linkage stamped onto an orchestrated session's metadata
// (acceptance: "Session metadata links job ID, trigger kind, run DB ID, attempt
// number, provider role, GitHub refs, PR links, and artifact IDs"). GitHub refs
// and artifact IDs are appended as the run progresses; the linkage is the cheap,
// listable index while the event log holds the full narrative.
type RunLink struct {
	JobID       string   `json:"job_id"`
	RunID       string   `json:"run_id"`
	TriggerKind string   `json:"trigger_kind,omitempty"`
	Attempt     int      `json:"attempt,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	Org         string   `json:"org,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	PRLinks     []string `json:"pr_links,omitempty"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
}

// PolicyDecision is the decoded payload of a KindPolicyDecision block.
type PolicyDecision struct {
	Policy   string `json:"policy"`           // e.g. merge_policy, permissions, budget
	Decision string `json:"decision"`         // e.g. approved, blocked, skipped
	Reason   string `json:"reason,omitempty"` // structured, never invented (§9)
}

// GitHubAction is the decoded payload of a KindGitHubAction block.
type GitHubAction struct {
	Action     string `json:"action"` // e.g. open_pr, comment, set_status, create_branch
	Repository string `json:"repository,omitempty"`
	Ref        string `json:"ref,omitempty"` // branch or commit
	PRNumber   int    `json:"pr_number,omitempty"`
	URL        string `json:"url,omitempty"` // PR/comment link
	// Outcome records whether the side effect succeeded ("ok") or failed
	// ("failed"); Error carries the failure detail. Both are additive (D2) so the
	// append-only session is a faithful record of hook success *and* failure
	// (AS-147), not only the actions that landed. Empty Outcome reads as a legacy
	// block written before the field existed (treat as succeeded).
	Outcome string `json:"outcome,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ArtifactRef is the decoded payload of a KindArtifactRef block: a large run
// artifact referenced by integrity hash and URI, never embedded (acceptance:
// "Large artifacts are integrity-checked and referenced rather than embedded").
type ArtifactRef struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	URI       string `json:"uri"`    // where the bytes live (opaque to core)
	SHA256    string `json:"sha256"` // integrity check over the referenced bytes
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

// RunOutcome is the decoded payload of a KindRunOutcome block.
type RunOutcome struct {
	Status       string  `json:"status"`
	FailureClass string  `json:"failure_class,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// newBlock builds a harness block of the given kind with its payload marshalled
// onto Ext under extKey. The payload is a fixed-shape struct, so marshalling
// never fails.
func newBlock(kind schema.Kind, payload any) schema.Block {
	raw, _ := json.Marshal(payload) //nolint:errcheck // fixed-shape struct never fails to marshal
	return schema.Block{
		ID:         schema.NewID(),
		Kind:       kind,
		Role:       schema.RoleHarness,
		Provenance: &schema.Provenance{Producer: producer},
		Ext:        map[string]json.RawMessage{extKey: raw},
	}
}

// payloadOf decodes an orchestration block's Ext payload into v, reporting false
// for any other kind or a payload that does not parse (defensive, mirroring
// eventlog.EscalationOf's tolerate-and-skip posture).
func payloadOf(b schema.Block, want schema.Kind, v any) bool {
	if b.Kind != want {
		return false
	}
	raw, ok := b.Ext[extKey]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// PolicyDecisionOf, GitHubActionOf, ArtifactRefOf, and RunOutcomeOf decode the
// respective orchestration blocks; each reports false for a non-matching kind or
// an unparseable payload so a reader (operator API AS-155) can fold over a mixed
// event stream without a type switch that must know every kind.
func PolicyDecisionOf(b schema.Block) (PolicyDecision, bool) {
	var d PolicyDecision
	return d, payloadOf(b, KindPolicyDecision, &d)
}

func GitHubActionOf(b schema.Block) (GitHubAction, bool) {
	var a GitHubAction
	return a, payloadOf(b, KindGitHubAction, &a)
}

func ArtifactRefOf(b schema.Block) (ArtifactRef, bool) {
	var a ArtifactRef
	return a, payloadOf(b, KindArtifactRef, &a)
}

func RunOutcomeOf(b schema.Block) (RunOutcome, bool) {
	var o RunOutcome
	return o, payloadOf(b, KindRunOutcome, &o)
}

// RunLinkOf decodes the identity linkage stamped on a session's metadata, if any.
func RunLinkOf(meta session.Metadata) (RunLink, bool) {
	var l RunLink
	raw, ok := meta.Ext[linkExtKey]
	if !ok {
		return l, false
	}
	return l, json.Unmarshal(raw, &l) == nil
}

// Recorder writes one orchestrated run as an ordinary Smith session. It is the
// AS-151 seam a real step Executor (AS-149/150) drives: create the session,
// append lifecycle blocks as steps run, then Finish with the terminal outcome.
// The Recorder holds the run's evolving linkage and rewrites it onto the session
// metadata as PR links and artifacts are added, so the listable index stays in
// step with the log.
type Recorder struct {
	sess *session.Session
	link RunLink
}

// NewRecorder creates a new Smith session for run, stamps its identity linkage
// onto the session metadata, and appends the opening KindRunStart block. job may
// be nil (the run store lost the spec); the linkage then carries only what the
// run row knows. The caller owns the returned Recorder's session log and must
// call Finish (which closes it).
func NewRecorder(st *session.Store, run store.Run, job *spec.Spec) (*Recorder, error) {
	link := RunLink{
		JobID:       run.JobID,
		RunID:       run.ID,
		TriggerKind: run.TriggerKind,
		Attempt:     run.Attempt,
	}
	if job != nil {
		link.Repository = job.Repository
		link.Org = job.Org
		link.Owner = job.Owner
	}
	raw, _ := json.Marshal(link) //nolint:errcheck // fixed-shape struct never fails to marshal
	title := fmt.Sprintf("orchestrated run %s (job %s)", run.ID, run.JobID)
	sess, err := st.CreateWith(title, map[string]json.RawMessage{linkExtKey: raw})
	if err != nil {
		return nil, err
	}
	r := &Recorder{sess: sess, link: link}
	if _, err := sess.Log.Append(newBlock(KindRunStart, link)); err != nil {
		_ = sess.Log.Close()
		return nil, err
	}
	return r, nil
}

// SessionID is the id of the session this run is recorded in; the executor writes
// it back into the run's terminal Outcome so the run store links to the session.
func (r *Recorder) SessionID() string { return r.sess.ID }

// PolicyDecision appends a policy-check verdict to the run's session.
func (r *Recorder) PolicyDecision(d PolicyDecision) error {
	_, err := r.sess.Log.Append(newBlock(KindPolicyDecision, d))
	return err
}

// GitHubAction appends a GitHub side effect to the run's session. When the action
// carries a PR link it is also folded into the session-metadata linkage so the
// operator surface can list the run's PRs without replaying the log.
func (r *Recorder) GitHubAction(a GitHubAction) error {
	if _, err := r.sess.Log.Append(newBlock(KindGitHubAction, a)); err != nil {
		return err
	}
	if a.URL != "" {
		return r.updateLink(func(l *RunLink) { l.PRLinks = append(l.PRLinks, a.URL) })
	}
	return nil
}

// Artifact references a large run artifact by integrity hash and URI. The bytes
// are never embedded — SHA256 and URI must be set so a consumer can fetch and
// verify them. The artifact id is folded into the session-metadata linkage.
func (r *Recorder) Artifact(a ArtifactRef) error {
	if a.URI == "" || a.SHA256 == "" {
		return fmt.Errorf("orchestrator: artifact %q needs a uri and sha256 (referenced, never embedded)", a.ID)
	}
	if _, err := r.sess.Log.Append(newBlock(KindArtifactRef, a)); err != nil {
		return err
	}
	return r.updateLink(func(l *RunLink) { l.ArtifactIDs = append(l.ArtifactIDs, a.ID) })
}

// Usage appends a provider-turn usage block so an orchestrated run's spend is
// priced by cost.Summarize through the same path as an interactive session. It is
// the seam the provider-step executor (AS-150) reports each turn's tokens through.
func (r *Recorder) Usage(vendor, model, stopReason string, tokens *schema.Tokens, meta *schema.UsageMeta) error {
	_, err := r.sess.Log.Append(eventlog.NewUsage(producer, vendor, model, stopReason, tokens, meta))
	return err
}

// Finish appends the terminal KindRunOutcome block and closes the session log.
// The returned session id is already available via SessionID; the caller copies
// it (and out.CostUSD) into the run store's Outcome.
func (r *Recorder) Finish(out store.Outcome) error {
	block := newBlock(KindRunOutcome, RunOutcome{
		Status:       string(out.Status),
		FailureClass: string(out.FailureClass),
		CostUSD:      out.CostUSD,
		Error:        out.Error,
	})
	if _, err := r.sess.Log.Append(block); err != nil {
		_ = r.sess.Log.Close()
		return err
	}
	return r.sess.Log.Close()
}

// updateLink applies mutate to a copy of the run linkage, persists it onto the
// session metadata, and commits the change to the in-memory linkage and metadata
// only after the write succeeds. Deferring the mutation keeps the in-memory state
// from drifting ahead of disk on a failed write — and avoids a double-append if a
// caller retries an action whose metadata write failed.
func (r *Recorder) updateLink(mutate func(*RunLink)) error {
	next := r.link
	next.PRLinks = append([]string(nil), r.link.PRLinks...)
	next.ArtifactIDs = append([]string(nil), r.link.ArtifactIDs...)
	mutate(&next)

	raw, _ := json.Marshal(next) //nolint:errcheck // fixed-shape struct never fails to marshal
	ext := make(map[string]json.RawMessage, len(r.sess.Metadata.Ext)+1)
	for k, v := range r.sess.Metadata.Ext {
		ext[k] = v
	}
	ext[linkExtKey] = raw
	meta := r.sess.Metadata
	meta.Ext = ext

	if err := session.WriteMetadata(r.sess.Dir, meta); err != nil {
		return err
	}
	r.link = next
	r.sess.Metadata = meta
	return nil
}
