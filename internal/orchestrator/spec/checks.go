package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// idRe is the job-id grammar (§4.1).
var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// checkInterpolation walks every interpolation-bearing string (concurrency.key
// and step/hook `with:` values), enforcing the closed variable namespace (rule
// 13), the secret-scope clause (rule 14), and the multi-trigger input clause
// (rule 15). It also runs the plaintext-secret scan (rule 14) over the same
// strings with interpolation stripped.
func (l *loader) checkInterpolation(s *Spec, triggerInputs []map[string]bool) {
	check := func(path, val string) {
		for _, v := range interpVars(val) {
			switch {
			case !knownInterp(v):
				l.add(13, path, "unknown interpolation variable ${%s}", v)
			case strings.HasPrefix(v, "secrets."):
				scope := strings.TrimPrefix(v, "secrets.")
				if !contains(s.Secrets, scope) {
					l.add(14, path, "${secrets.%s} names a scope not listed under secrets", scope)
				}
			case strings.HasPrefix(v, "trigger.inputs."):
				input := strings.TrimPrefix(v, "trigger.inputs.")
				l.checkTriggerInput(path, input, triggerInputs)
			}
		}
		if looksLikeSecret(stripInterp(val)) {
			l.add(14, path, "value looks like a plaintext credential; secrets are declared as scope names, never values")
		}
	}

	check("concurrency.key", s.Concurrency.Key)
	for i, st := range s.Steps {
		walkStrings(fmt.Sprintf("steps[%d].with", i), st.With, check)
	}
	for _, point := range sortedKeys(s.Hooks) {
		for i, st := range s.Hooks[point] {
			walkStrings(fmt.Sprintf("hooks.%s[%d].with", point, i), st.With, check)
		}
	}
	// Plaintext scan over other free strings that could carry a leaked secret.
	if looksLikeSecret(s.Description) {
		l.add(14, "description", "description looks like a plaintext credential")
	}
}

// checkTriggerInput enforces rule 15: a ${trigger.inputs.X} reference requires
// every trigger on the job to declare input X (otherwise it is undefined when a
// trigger that lacks it fires).
func (l *loader) checkTriggerInput(path, input string, triggerInputs []map[string]bool) {
	if len(triggerInputs) == 0 {
		l.add(15, path, "${trigger.inputs.%s} referenced but the job declares no triggers with inputs", input)
		return
	}
	for _, declared := range triggerInputs {
		if !declared[input] {
			l.add(15, path, "${trigger.inputs.%s} must be declared by every trigger (a trigger that lacks it would be undefined at runtime)", input)
			return
		}
	}
}

// checkLabels enforces rule 8: every label any trigger or step/hook references
// must appear in known_labels.
func (l *loader) checkLabels(s *Spec) {
	known := set(s.KnownLabels...)
	ref := func(path, label string) {
		if label != "" && !known[label] {
			l.add(8, path, "label %q is not declared in known_labels", label)
		}
	}
	for i, t := range s.Triggers {
		if label, ok := asString(t.Args["label"]); ok {
			ref(fmt.Sprintf("triggers[%d].label", i), label)
		}
	}
	labelFromStep := func(path string, st Step) {
		if st.With == nil {
			return
		}
		if label, ok := asString(st.With["label"]); ok {
			ref(path+".with.label", label)
		}
	}
	for i, st := range s.Steps {
		labelFromStep(fmt.Sprintf("steps[%d]", i), st)
	}
	for _, point := range sortedKeys(s.Hooks) {
		for i, st := range s.Hooks[point] {
			labelFromStep(fmt.Sprintf("hooks.%s[%d]", point, i), st)
		}
	}
	// merge_policy label_present predicates reference labels too.
	if s.MergePolicy != nil {
		for _, p := range s.MergePolicy.Required {
			if p.Name == "label_present" {
				if label, ok := asString(p.Arg); ok {
					ref("merge_policy.required.label_present", label)
				}
			}
		}
	}
}

// checkSecretBindings enforces rule 9 via implicit binding (§4.6): a github.*
// action needs the github-token scope, and an agent.* step whose provider_policy
// resolves to a known provider needs that provider's api-key scope. Explicit
// ${secrets.*} references are covered by checkInterpolation (rule 14).
func (l *loader) checkSecretBindings(s *Spec) {
	has := set(s.Secrets...)
	need := func(path, scope, why string) {
		if !has[scope] {
			l.add(9, path, "%s requires secret scope %q, which is not listed under secrets", why, scope)
		}
	}
	all := append([]Step(nil), s.Steps...)
	for _, point := range sortedKeys(s.Hooks) {
		all = append(all, s.Hooks[point]...)
	}
	githubFlagged := false
	for i, st := range all {
		path := fmt.Sprintf("steps[%d]", i)
		if strings.HasPrefix(st.Uses, "github.") && !githubFlagged {
			need(path, "github-token", "github.* actions")
			githubFlagged = true // one report is enough; the fix is the same
		}
		if strings.HasPrefix(st.Uses, "agent.") && st.ProviderPolicy != "" {
			if r, ok := s.Routing[st.ProviderPolicy]; ok {
				if scope, ok := providerScope[r.Provider]; ok {
					need(path, scope, fmt.Sprintf("agent step routed to %q", r.Provider))
				}
			}
		}
	}
}

// checkProviderPolicies enforces rule 10: a step's provider_policy must resolve
// to a declared routing entry.
func (l *loader) checkProviderPolicies(s *Spec) {
	for i, st := range s.Steps {
		if st.ProviderPolicy == "" {
			continue
		}
		if _, ok := s.Routing[st.ProviderPolicy]; !ok {
			l.add(10, fmt.Sprintf("steps[%d].provider_policy", i),
				"provider_policy %q has no matching routing entry", st.ProviderPolicy)
		}
	}
}

// checkMergeRequirement enforces rule 12's first clause: a step or hook that
// enables auto-merge or merges requires a merge_policy.
func (l *loader) checkMergeRequirement(s *Spec) {
	enabling := func(st Step) bool { return mergeEnablingActions[st.Uses] }
	found := false
	for _, st := range s.Steps {
		found = found || enabling(st)
	}
	for _, point := range sortedKeys(s.Hooks) {
		for _, st := range s.Hooks[point] {
			found = found || enabling(st)
		}
	}
	if found && s.MergePolicy == nil {
		l.add(12, "merge_policy", "a step or hook enables merge but no merge_policy is declared (fail-closed)")
	}
}

// checkBudgets enforces rule 7's second clause: summed step budgets must not
// exceed budget.run.
func (l *loader) checkBudgets(s *Spec) {
	var sum float64
	for _, st := range s.Steps {
		sum += st.Budget
	}
	if s.Budget.Run > 0 && sum > s.Budget.Run+1e-9 {
		l.add(7, "budget.run", "step budgets sum to %.2f, exceeding budget.run %.2f", sum, s.Budget.Run)
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// walkStrings invokes fn for every string value reachable in v (recursing into
// maps and slices), building a dotted/indexed path for error messages.
func walkStrings(path string, v any, fn func(path, val string)) {
	switch t := v.(type) {
	case string:
		fn(path, t)
	case map[string]any:
		for _, k := range sortedKeys(t) {
			walkStrings(path+"."+k, t[k], fn)
		}
	case []any:
		for i, e := range t {
			walkStrings(fmt.Sprintf("%s[%d]", path, i), e, fn)
		}
	}
}
