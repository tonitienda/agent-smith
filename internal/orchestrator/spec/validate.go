package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Closed vocabularies. The format is fail-closed (§3, §5 rule 1): any key,
// kind, action, or value outside these sets is a validation error, never a
// silently-tolerated extension.
var (
	topLevelKeys = set("id", "version", "owner", "repository", "org", "description",
		"triggers", "concurrency", "timeout", "retries", "budget", "permissions",
		"known_labels", "secrets", "routing", "steps", "hooks", "merge_policy", "retention")

	stepKeys = set("id", "uses", "role", "provider_policy", "budget", "when", "with")

	triggerKinds = set("cron", "manual", "github.issue_labeled", "github.pr_labeled",
		"github.pr_merged", "github.comment_command", "followup")

	// MVP-0 action catalogue (§4.7). Semantics live in AS-147/AS-149; this layer
	// only fixes the call shape and the agent-vs-deterministic split.
	agentActions = set("agent.implement", "agent.review", "agent.architecture_check",
		"agent.manual_test_sim")
	githubActions = set("github.add_label", "github.remove_label", "github.create_or_update_pr",
		"github.comment", "github.set_status", "github.enable_auto_merge", "github.merge")
	mergeEnablingActions = set("github.enable_auto_merge", "github.merge")

	githubPermResources = set("contents", "pull_requests", "issues", "checks",
		"statuses", "actions", "metadata")
	permValues = set("read", "write")

	onConflictValues = set("queue", "cancel-running", "drop")
	backoffValues    = set("fixed", "exponential")
	mergeModeValues  = set("off", "auto", "manual")

	// Merge predicate catalogue (§4.10). The closed set is owned by AS-157; the
	// forbidden invariants can never be expressed as *required* (permitted).
	mergeRequiredPredicates = set("pr_author_is_smith", "required_checks_green", "label_present")
	forbiddenInvariants     = set("unknown_checks", "branch_protection_bypass", "force_push")

	// providerScope maps a routing provider to the secret scope an agent step
	// resolved to it needs (implicit binding, §4.6). Unknown providers get no
	// implicit check — their scope resolution is AS-150/AS-154's job.
	providerScope = map[string]string{"anthropic": "anthropic-api-key", "openai": "openai-api-key"}
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Load decodes and validates a single job spec from an already-decoded generic
// map (the shape gopkg.in/yaml.v3 or encoding/json produce). It returns the
// typed *Spec and every validation error it found; a non-empty Errors means the
// daemon must refuse to schedule the job (fail-closed). The *Spec is still
// returned (best-effort populated) so a tool can show partial structure, but
// only a Load with no errors yields a spec safe to run.
func Load(file string, raw map[string]any) (*Spec, Errors) {
	l := &loader{file: file}
	s := &Spec{File: file}

	l.closedKeys("", raw, topLevelKeys, 1)

	s.ID = l.string(raw, "id", true, 2)
	if s.ID != "" && !idRe.MatchString(s.ID) {
		l.add(2, "id", "id %q must match %s", s.ID, idRe)
	}
	s.Version = l.int(raw, "version", true, 3)
	if _, ok := raw["version"]; ok && s.Version != SupportedVersion {
		l.add(3, "version", "version %d is unsupported; this loader understands version %d", s.Version, SupportedVersion)
	}
	s.Owner = l.string(raw, "owner", true, 1)
	s.Repository = l.string(raw, "repository", false, 4)
	s.Org = l.string(raw, "org", false, 4)
	if (s.Repository == "") == (s.Org == "") {
		l.add(4, "repository/org", "exactly one of repository or org must be set")
	}
	s.Description = l.string(raw, "description", false, 1)

	triggerInputs := l.loadTriggers(s, raw)
	l.loadConcurrency(s, raw)
	s.Timeout = l.duration(raw, "timeout", true, 16)
	l.loadRetries(s, raw)
	l.loadBudget(s, raw)
	l.loadPermissions(s, raw)
	s.KnownLabels = l.stringList(raw, "known_labels")
	s.Secrets = l.stringList(raw, "secrets")
	l.loadRouting(s, raw)
	l.loadSteps(s, raw)
	l.loadHooks(s, raw)
	l.loadMergePolicy(s, raw)
	l.loadRetention(s, raw)

	// Cross-field checks that need the whole spec assembled.
	l.checkInterpolation(s, triggerInputs)
	l.checkLabels(s)
	l.checkSecretBindings(s)
	l.checkProviderPolicies(s)
	l.checkMergeRequirement(s)
	l.checkBudgets(s)

	sort.SliceStable(l.errs, func(i, j int) bool {
		if l.errs[i].Path != l.errs[j].Path {
			return l.errs[i].Path < l.errs[j].Path
		}
		return l.errs[i].Rule < l.errs[j].Rule
	})
	return s, l.errs
}

// loader accumulates validation errors against one file.
type loader struct {
	file string
	errs Errors
}

func (l *loader) add(rule int, path, format string, args ...any) {
	l.errs = append(l.errs, Error{File: l.file, Path: path, Rule: rule, Msg: fmt.Sprintf(format, args...)})
}

// closedKeys reports any key in m outside allowed under rule (1). prefix is the
// field path of the enclosing map for error messages.
func (l *loader) closedKeys(prefix string, m map[string]any, allowed map[string]bool, rule int) {
	for k := range m {
		if !allowed[k] {
			l.add(rule, joinPath(prefix, k), "unknown key %q", k)
		}
	}
}

func (l *loader) string(m map[string]any, key string, required bool, rule int) string {
	v, ok := m[key]
	if !ok {
		if required {
			l.add(rule, key, "missing required %q", key)
		}
		return ""
	}
	s, ok := asString(v)
	if !ok {
		l.add(rule, key, "%q must be a string", key)
		return ""
	}
	return s
}

func (l *loader) int(m map[string]any, key string, required bool, rule int) int {
	v, ok := m[key]
	if !ok {
		if required {
			l.add(rule, key, "missing required %q", key)
		}
		return 0
	}
	n, ok := asInt(v)
	if !ok {
		l.add(rule, key, "%q must be an integer", key)
		return 0
	}
	return n
}

func (l *loader) duration(m map[string]any, key string, required bool, rule int) Duration {
	v, ok := m[key]
	if !ok {
		if required {
			l.add(rule, key, "missing required duration %q", key)
		}
		return Duration{}
	}
	s, ok := asString(v)
	if !ok {
		l.add(rule, key, "duration %q must be a string", key)
		return Duration{}
	}
	d, err := ParseDuration(s)
	if err != nil {
		l.add(rule, key, "%v", err)
	}
	return d
}

func (l *loader) stringList(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	xs, ok := asSlice(v)
	if !ok {
		l.add(1, key, "%q must be a list", key)
		return nil
	}
	out := make([]string, 0, len(xs))
	for i, e := range xs {
		s, ok := asString(e)
		if !ok {
			l.add(1, fmt.Sprintf("%s[%d]", key, i), "must be a string")
			continue
		}
		out = append(out, s)
	}
	return out
}

func (l *loader) loadTriggers(s *Spec, raw map[string]any) (declaredInputs []map[string]bool) {
	v, ok := raw["triggers"]
	if !ok {
		l.add(5, "triggers", "missing required triggers (>=1)")
		return nil
	}
	xs, ok := asSlice(v)
	if !ok {
		l.add(5, "triggers", "triggers must be a list")
		return nil
	}
	if len(xs) == 0 {
		l.add(5, "triggers", "triggers must have at least one entry")
		return nil
	}
	for i, e := range xs {
		path := fmt.Sprintf("triggers[%d]", i)
		m, ok := asMap(e)
		if !ok || len(m) != 1 {
			l.add(5, path, "each trigger must be a single-key map naming the kind")
			declaredInputs = append(declaredInputs, nil)
			continue
		}
		kind := onlyKey(m)
		if !triggerKinds[kind] {
			l.add(1, path, "unknown trigger kind %q", kind)
			declaredInputs = append(declaredInputs, nil)
			continue
		}
		args, _ := asMap(m[kind])
		t := Trigger{Kind: kind, Args: args}
		s.Triggers = append(s.Triggers, t)
		declaredInputs = append(declaredInputs, l.validateTrigger(path, kind, args))
	}
	return declaredInputs
}

// validateTrigger checks kind-specific required args (§4.2 / rule 5) and returns
// the set of input names the trigger declares (for the rule-15 cross-check).
func (l *loader) validateTrigger(path, kind string, args map[string]any) map[string]bool {
	inputs := map[string]bool{}
	switch kind {
	case "cron":
		tz, ok := asString(args["timezone"])
		if !ok || tz == "" {
			l.add(5, path+".timezone", "cron trigger requires an IANA timezone")
		} else if strings.HasPrefix(tz, "+") || strings.HasPrefix(tz, "-") || strings.Contains(tz, ":") {
			l.add(5, path+".timezone", "cron timezone %q must be an IANA name, not a bare offset", tz)
		}
		if sch, ok := asString(args["schedule"]); !ok || sch == "" {
			l.add(5, path+".schedule", "cron trigger requires a schedule")
		}
	case "manual":
		if im, ok := asMap(args["inputs"]); ok {
			for name := range im {
				inputs[name] = true
			}
		}
	case "followup":
		if n, ok := asInt(args["max_runs"]); !ok || n < 1 {
			l.add(5, path+".max_runs", "followup trigger requires max_runs >= 1 (no unbounded follow-up)")
		}
	}
	return inputs
}

func (l *loader) loadConcurrency(s *Spec, raw map[string]any) {
	m, ok := asMap(raw["concurrency"])
	if !ok {
		l.add(6, "concurrency", "missing required concurrency block")
		return
	}
	l.closedKeys("concurrency", m, set("key", "limit", "on_conflict"), 1)
	s.Concurrency.Key, _ = asString(m["key"])
	n, ok := asInt(m["limit"])
	if !ok || n < 1 {
		l.add(6, "concurrency.limit", "concurrency.limit is required and must be >= 1 (no unbounded concurrency)")
	}
	s.Concurrency.Limit = n
	oc, ok := asString(m["on_conflict"])
	if !ok || oc == "" {
		oc = "queue"
	} else if !onConflictValues[oc] {
		l.add(1, "concurrency.on_conflict", "on_conflict %q must be queue|cancel-running|drop", oc)
	}
	s.Concurrency.OnConflict = oc
}

func (l *loader) loadRetries(s *Spec, raw map[string]any) {
	v, ok := raw["retries"]
	if !ok {
		return
	}
	m, ok := asMap(v)
	if !ok {
		l.add(1, "retries", "retries must be a map")
		return
	}
	l.closedKeys("retries", m, set("max", "backoff", "initial"), 1)
	r := &Retries{Backoff: "exponential"}
	r.Max = l.int(m, "max", false, 1)
	if r.Max < 0 {
		l.add(1, "retries.max", "retries.max must be >= 0")
	}
	if b, ok := asString(m["backoff"]); ok && b != "" {
		if !backoffValues[b] {
			l.add(1, "retries.backoff", "backoff %q must be fixed|exponential", b)
		}
		r.Backoff = b
	}
	if r.Max > 0 {
		r.Initial = l.duration(m, "initial", true, 16)
	} else if _, ok := m["initial"]; ok {
		r.Initial = l.duration(m, "initial", false, 16)
	}
	s.Retries = r
}

func (l *loader) loadBudget(s *Spec, raw map[string]any) {
	m, ok := asMap(raw["budget"])
	if !ok {
		l.add(7, "budget", "missing required budget block (budget.run is required)")
		return
	}
	l.closedKeys("budget", m, set("run", "monthly"), 1)
	run, ok := asFloat(m["run"])
	if !ok || run <= 0 {
		l.add(7, "budget.run", "budget.run is required and must be > 0")
	}
	s.Budget.Run = run
	if mv, ok := m["monthly"]; ok {
		mon, ok := asFloat(mv)
		if !ok || mon <= 0 {
			l.add(7, "budget.monthly", "budget.monthly must be > 0 when set")
		}
		s.Budget.Monthly = mon
	}
}

func (l *loader) loadPermissions(s *Spec, raw map[string]any) {
	v, ok := raw["permissions"]
	if !ok {
		l.add(1, "permissions", "missing required permissions block (may be empty, but explicit)")
		return
	}
	m, ok := asMap(v)
	if !ok {
		l.add(1, "permissions", "permissions must be a map")
		return
	}
	l.closedKeys("permissions", m, set("github"), 1)
	s.Permissions.GitHub = map[string]string{}
	if gh, ok := asMap(m["github"]); ok {
		for res, av := range gh {
			if !githubPermResources[res] {
				l.add(1, "permissions.github."+res, "unknown GitHub permission resource %q", res)
				continue
			}
			access, ok := asString(av)
			if !ok || !permValues[access] {
				l.add(1, "permissions.github."+res, "permission %q must be read|write", res)
				continue
			}
			s.Permissions.GitHub[res] = access
		}
	}
}

func (l *loader) loadRouting(s *Spec, raw map[string]any) {
	v, ok := raw["routing"]
	if !ok {
		return
	}
	m, ok := asMap(v)
	if !ok {
		l.add(1, "routing", "routing must be a map of named policies")
		return
	}
	s.Routing = map[string]Route{}
	for name, rv := range m {
		rm, ok := asMap(rv)
		if !ok {
			l.add(1, "routing."+name, "routing entry must be a map")
			continue
		}
		l.closedKeys("routing."+name, rm, set("provider", "model"), 1)
		s.Routing[name] = Route{
			Provider: l.string(rm, "provider", true, 1),
			Model:    l.string(rm, "model", true, 1),
		}
	}
}

func (l *loader) loadSteps(s *Spec, raw map[string]any) {
	v, ok := raw["steps"]
	if !ok {
		l.add(1, "steps", "missing required steps (>=1)")
		return
	}
	xs, ok := asSlice(v)
	if !ok || len(xs) == 0 {
		l.add(1, "steps", "steps must be a non-empty list")
		return
	}
	seen := map[string]bool{}
	for i, e := range xs {
		path := fmt.Sprintf("steps[%d]", i)
		st := l.loadStep(path, e)
		if st.ID == "" {
			l.add(1, path+".id", "step requires an id")
		} else {
			if seen[st.ID] {
				l.add(1, path+".id", "duplicate step id %q", st.ID)
			}
			seen[st.ID] = true
		}
		s.Steps = append(s.Steps, st)
	}
}

func (l *loader) loadStep(path string, e any) Step {
	m, ok := asMap(e)
	if !ok {
		l.add(1, path, "step must be a map")
		return Step{}
	}
	l.closedKeys(path, m, stepKeys, 1)
	st := Step{
		ID:             l.string(m, "id", false, 1),
		Uses:           l.string(m, "uses", true, 1),
		Role:           l.string(m, "role", false, 11),
		ProviderPolicy: l.string(m, "provider_policy", false, 10),
		When:           l.string(m, "when", false, 13),
	}
	if bv, ok := m["budget"]; ok {
		b, ok := asFloat(bv)
		if !ok || b <= 0 {
			l.add(7, path+".budget", "step budget must be > 0 when set")
		}
		st.Budget = b
	}
	if wm, ok := asMap(m["with"]); ok {
		st.With = wm
	} else if _, ok := m["with"]; ok {
		l.add(1, path+".with", "with must be a map")
	}
	l.validateAction(path, st)
	if st.When != "" {
		l.validateWhen(path+".when", st.When)
	}
	return st
}

// validateAction enforces the action catalogue and the agent-vs-deterministic
// split (rules 1, 11).
func (l *loader) validateAction(path string, st Step) {
	switch {
	case agentActions[st.Uses]:
		if st.Role == "" {
			l.add(11, path+".role", "agent action %q requires a role", st.Uses)
		}
	case githubActions[st.Uses]:
		if st.Role != "" {
			l.add(11, path+".role", "deterministic action %q must not declare a role", st.Uses)
		}
	case st.Uses == "":
		// already reported missing uses
	default:
		l.add(1, path+".uses", "unknown action %q", st.Uses)
	}
}

// validateWhen enforces rule 13: a single boolean identifier from the closed
// namespace policy.* / trigger.* / steps.<id>.outcome, with no operators.
func (l *loader) validateWhen(path, when string) {
	if strings.ContainsAny(when, " \t&|!()<>=+,") {
		l.add(13, path, "when guard %q must be a single identifier with no operators", when)
		return
	}
	ok := strings.HasPrefix(when, "policy.") || strings.HasPrefix(when, "trigger.") ||
		(strings.HasPrefix(when, "steps.") && strings.HasSuffix(when, ".outcome"))
	if !ok {
		l.add(13, path, "when guard %q must name policy.*, trigger.*, or steps.<id>.outcome", when)
	}
}

func (l *loader) loadHooks(s *Spec, raw map[string]any) {
	v, ok := raw["hooks"]
	if !ok {
		return
	}
	m, ok := asMap(v)
	if !ok {
		l.add(1, "hooks", "hooks must be a map of lifecycle points")
		return
	}
	allowed := set(HookPoints...)
	s.Hooks = map[string][]Step{}
	for point, hv := range m {
		if !allowed[point] {
			l.add(1, "hooks."+point, "unknown hook point %q", point)
			continue
		}
		xs, ok := asSlice(hv)
		if !ok {
			l.add(1, "hooks."+point, "hook %q must be a list of steps", point)
			continue
		}
		for i, e := range xs {
			path := fmt.Sprintf("hooks.%s[%d]", point, i)
			st := l.loadStep(path, e)
			if agentActions[st.Uses] {
				l.add(1, path+".uses", "hooks may only run deterministic github.* actions, not agent action %q", st.Uses)
			}
			s.Hooks[point] = append(s.Hooks[point], st)
		}
	}
}

func (l *loader) loadMergePolicy(s *Spec, raw map[string]any) {
	v, ok := raw["merge_policy"]
	if !ok {
		return
	}
	m, ok := asMap(v)
	if !ok {
		l.add(1, "merge_policy", "merge_policy must be a map")
		return
	}
	l.closedKeys("merge_policy", m, set("mode", "required", "forbidden"), 1)
	mp := &MergePolicy{Mode: "off"}
	if mode, ok := asString(m["mode"]); ok && mode != "" {
		if !mergeModeValues[mode] {
			l.add(1, "merge_policy.mode", "mode %q must be off|auto|manual", mode)
		}
		mp.Mode = mode
	}
	mp.Required = l.predicateList(m, "required", "merge_policy.required")
	mp.Forbidden = l.predicateList(m, "forbidden", "merge_policy.forbidden")
	// Rule 12: a forbidden-invariant predicate may never be expressed as required
	// (permitted). The format must not be able to allow a protection bypass.
	for _, p := range mp.Required {
		if forbiddenInvariants[p.Name] {
			l.add(12, "merge_policy.required", "predicate %q is a protection invariant and cannot be permitted (list it under forbidden)", p.Name)
		} else if !mergeRequiredPredicates[p.Name] {
			l.add(12, "merge_policy.required", "unknown merge predicate %q", p.Name)
		}
	}
	for _, p := range mp.Forbidden {
		if !forbiddenInvariants[p.Name] {
			l.add(12, "merge_policy.forbidden", "unknown forbidden predicate %q", p.Name)
		}
	}
	s.MergePolicy = mp
}

// predicateList reads a uniform list of single-key maps (rule 17).
func (l *loader) predicateList(m map[string]any, key, path string) []Predicate {
	v, ok := m[key]
	if !ok {
		return nil
	}
	xs, ok := asSlice(v)
	if !ok {
		l.add(17, path, "%s must be a list of single-key maps", key)
		return nil
	}
	var out []Predicate
	for i, e := range xs {
		pm, ok := asMap(e)
		if !ok || len(pm) != 1 {
			l.add(17, fmt.Sprintf("%s[%d]", path, i), "each %s item must be a single-key map", key)
			continue
		}
		name := onlyKey(pm)
		out = append(out, Predicate{Name: name, Arg: pm[name]})
	}
	return out
}

func (l *loader) loadRetention(s *Spec, raw map[string]any) {
	v, ok := raw["retention"]
	if !ok {
		return
	}
	m, ok := asMap(v)
	if !ok {
		l.add(1, "retention", "retention must be a map")
		return
	}
	l.closedKeys("retention", m, set("runs", "artifacts"), 1)
	r := &Retention{}
	if _, ok := m["runs"]; ok {
		r.Runs = l.duration(m, "runs", false, 16)
	}
	if _, ok := m["artifacts"]; ok {
		r.Artifacts = l.duration(m, "artifacts", false, 16)
	}
	s.Retention = r
}
