// Package spec is the orchestrator job-spec model and validator: it turns the
// declarative `.agent-smith/jobs/*.yaml` format frozen in
// docs/design/job-spec-dsl.md (AS-160) into typed Go values and enforces the
// normative validation rules (§5) at load time.
//
// It is decoding-agnostic on purpose. [Load] takes an already-decoded generic
// map (the shape both encoding/json and gopkg.in/yaml.v3 produce when decoding
// into interface{}), so the package stays stdlib-only and the YAML-file plumbing
// lives with the daemon (AS-161). Everything downstream — GitHub actions
// (AS-147/AS-149), routing (AS-150), session-log integration (AS-151), the
// dogfood pack (AS-152), secret scopes (AS-154), and merge gating (AS-157) —
// consumes the typed [Spec] and never re-parses the format.
//
// Validation is fail-closed (ADR D-ORCH-1/D-ORCH-6): an underspecified or
// unknown construct is an error, never a silently-ignored extension. Every
// [Error] names the file, the field path, and the rule number so a rejection
// reads like a review comment rather than a stack trace.
package spec

import (
	"fmt"
	"sort"
)

// Spec is a validated job specification. Fields mirror the top-level shape in
// docs/design/job-spec-dsl.md §3; a *Spec only exists once [Load] returned no
// errors, so consumers may treat every required field as present.
type Spec struct {
	File        string
	ID          string
	Version     int
	Owner       string
	Repository  string // xor Org
	Org         string // xor Repository
	Description string

	Triggers    []Trigger
	Concurrency Concurrency
	Timeout     Duration
	Retries     *Retries
	Budget      Budget
	Permissions Permissions
	KnownLabels []string
	Secrets     []string
	Routing     map[string]Route
	Steps       []Step
	Hooks       map[string][]Step // keyed by HookPoints
	MergePolicy *MergePolicy
	Retention   *Retention
}

// Trigger is one entry of the triggers list: a single-key map naming the kind
// (§4.2). Args holds the kind-specific arguments verbatim for downstream
// consumers; the validator only checks the structure this layer owns.
type Trigger struct {
	Kind string
	Args map[string]any
}

// Concurrency is the required concurrency block (§4.3). Limit is always >= 1.
type Concurrency struct {
	Key        string
	Limit      int
	OnConflict string // queue | cancel-running | drop
}

// Retries is the optional retry policy (§4.4).
type Retries struct {
	Max     int
	Backoff string // fixed | exponential
	Initial Duration
}

// Budget is the required budget block (§4.5); Run is the per-run USD ceiling.
type Budget struct {
	Run     float64
	Monthly float64 // 0 when omitted
}

// Permissions is the explicit permissions block (§4.6). GitHub maps a GitHub
// resource to read|write.
type Permissions struct {
	GitHub map[string]string
}

// Route is a named routing policy (§4.8) referenced by a step's provider_policy.
type Route struct {
	Provider string
	Model    string
}

// Step is one entry of steps or a hook list (§4.7). Exactly the keys
// id/uses/role/provider_policy/budget/when/with are allowed on a step.
type Step struct {
	ID             string
	Uses           string
	Role           string
	ProviderPolicy string
	Budget         float64 // 0 when omitted
	When           string
	With           map[string]any
}

// MergePolicy is the optional merge policy (§4.10); required whenever any step
// or hook enables auto-merge or merge.
type MergePolicy struct {
	Mode      string // off | auto | manual
	Required  []Predicate
	Forbidden []Predicate
}

// Predicate is one uniform single-key map from merge_policy.required/forbidden.
type Predicate struct {
	Name string
	Arg  any
}

// Retention bounds run-store rows and artifacts (§4.11).
type Retention struct {
	Runs      Duration
	Artifacts Duration
}

// HookPoints are the lifecycle points hooks may declare (§4.9).
var HookPoints = []string{"on_start", "on_success", "on_failure", "on_cancel"}

// SupportedVersion is the only DSL version this loader understands. A spec
// declaring any other version is rejected (rule 3); new versions arrive
// additively per docs/design/job-spec-dsl.md §7.
const SupportedVersion = 1

// Error is a single validation failure tied to a file, a field path, and the
// §5 rule it violates.
type Error struct {
	File string
	Path string
	Rule int
	Msg  string
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s: rule %d: %s", e.File, e.Path, e.Rule, e.Msg)
}

// Errors is a sorted, joinable collection of validation failures.
type Errors []Error

func (es Errors) Error() string {
	switch len(es) {
	case 0:
		return "<no error>"
	case 1:
		return es[0].Error()
	default:
		return fmt.Sprintf("%s (and %d more)", es[0].Error(), len(es)-1)
	}
}

// CheckUnique enforces rule 2's cross-spec clause: two loaded specs may not
// declare the same id. It is the daemon's job (AS-161) to call this over the
// specs that survived per-file [Load]; single-file validation cannot see
// collisions. Returned errors are attached to the second file that uses an id.
func CheckUnique(specs []*Spec) Errors {
	seen := map[string]string{} // id -> first file
	var errs Errors
	// Stable order so messages are deterministic regardless of map iteration.
	ordered := append([]*Spec(nil), specs...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].File < ordered[j].File })
	for _, s := range ordered {
		if first, ok := seen[s.ID]; ok {
			errs = append(errs, Error{
				File: s.File, Path: "id", Rule: 2,
				Msg: fmt.Sprintf("id %q already declared by %s", s.ID, first),
			})
			continue
		}
		seen[s.ID] = s.File
	}
	return errs
}
