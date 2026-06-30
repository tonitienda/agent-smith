package spec

import (
	"encoding/json"
	"testing"
)

// decode turns a JSON spec literal into the generic map [Load] consumes. JSON is
// a YAML subset for the value shapes we use, so a JSON fixture exercises the same
// decoding-agnostic path the daemon's yaml.v3 loader will feed in (numbers land
// as float64 here, which asInt/asFloat handle exactly as they handle yaml ints).
func decode(t *testing.T, js string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	return m
}

// validImplement is the canonical implementation job (DSL §4.1 / §6.3 shape),
// expressed as JSON. Tests mutate a decoded copy of it to drive each rule.
const validImplement = `{
  "id": "implement-labeled-work",
  "version": 1,
  "owner": "maintainer",
  "repository": "tonitienda/agent-smith",
  "description": "Implement issues labeled for the bot.",
  "triggers": [
    {"github.issue_labeled": {"label": "implementation"}},
    {"github.pr_labeled": {"label": "implementation"}}
  ],
  "concurrency": {"key": "repo:${repository}:implementation", "limit": 1, "on_conflict": "queue"},
  "timeout": "45m",
  "retries": {"max": 2, "backoff": "exponential", "initial": "30s"},
  "budget": {"run": 6.0, "monthly": 200.0},
  "permissions": {"github": {"contents": "write", "pull_requests": "write", "issues": "write", "checks": "read"}},
  "known_labels": ["implementation", "smith-generated", "smith-auto-merge"],
  "secrets": ["github-token", "anthropic-api-key", "openai-api-key"],
  "routing": {
    "anthropic-impl": {"provider": "anthropic", "model": "claude-opus-4-8"},
    "gpt-review": {"provider": "openai", "model": "gpt-5-review"}
  },
  "steps": [
    {"id": "implement", "uses": "agent.implement", "role": "implementation", "provider_policy": "anthropic-impl", "budget": 4.0},
    {"id": "review", "uses": "agent.review", "role": "review", "provider_policy": "gpt-review", "budget": 2.0},
    {"id": "open-pr", "uses": "github.create_or_update_pr"},
    {"id": "mark", "uses": "github.add_label", "with": {"label": "smith-generated"}},
    {"id": "automerge", "uses": "github.enable_auto_merge", "when": "policy.auto_merge_allowed"}
  ],
  "hooks": {
    "on_failure": [{"uses": "github.comment", "with": {"body_template": "run-failed"}}]
  },
  "merge_policy": {
    "mode": "auto",
    "required": [
      {"pr_author_is_smith": true},
      {"required_checks_green": true},
      {"label_present": "smith-generated"},
      {"label_present": "smith-auto-merge"}
    ],
    "forbidden": [
      {"unknown_checks": true},
      {"branch_protection_bypass": true},
      {"force_push": true}
    ]
  },
  "retention": {"runs": "90d", "artifacts": "30d"}
}`

func TestLoadValidSpec(t *testing.T) {
	s, errs := Load("implement.yaml", decode(t, validImplement))
	if len(errs) != 0 {
		t.Fatalf("valid spec rejected: %v", errs)
	}
	if s.ID != "implement-labeled-work" || s.Version != 1 {
		t.Fatalf("identity not parsed: %+v", s)
	}
	if got := len(s.Steps); got != 5 {
		t.Fatalf("want 5 steps, got %d", got)
	}
	if s.Timeout.Std().Minutes() != 45 {
		t.Fatalf("timeout parse: %v", s.Timeout)
	}
	if s.Retention == nil || s.Retention.Runs.Std().Hours() != 90*24 {
		t.Fatalf("retention 90d parse: %+v", s.Retention)
	}
	if s.Concurrency.OnConflict != "queue" {
		t.Fatalf("concurrency default: %+v", s.Concurrency)
	}
}

// mutate decodes the canonical spec and applies fn so each case starts valid and
// breaks exactly one thing.
func mutate(t *testing.T, fn func(m map[string]any)) map[string]any {
	t.Helper()
	m := decode(t, validImplement)
	fn(m)
	return m
}

func hasRule(errs Errors, rule int) bool {
	for _, e := range errs {
		if e.Rule == rule {
			return true
		}
	}
	return false
}

func TestValidationRules(t *testing.T) {
	cases := []struct {
		name string
		rule int
		fn   func(m map[string]any)
	}{
		{"unknown top-level key", 1, func(m map[string]any) { m["surprise"] = true }},
		{"unknown step key", 1, func(m map[string]any) {
			m["steps"].([]any)[0].(map[string]any)["label"] = "x"
		}},
		{"unknown trigger kind", 1, func(m map[string]any) {
			m["triggers"] = []any{map[string]any{"github.never": map[string]any{}}}
		}},
		{"unknown action", 1, func(m map[string]any) {
			m["steps"].([]any)[2].(map[string]any)["uses"] = "github.teleport"
		}},
		{"id malformed", 2, func(m map[string]any) { m["id"] = "Bad_ID" }},
		{"id missing", 2, func(m map[string]any) { delete(m, "id") }},
		{"version unsupported", 3, func(m map[string]any) { m["version"] = 99.0 }},
		{"version missing", 3, func(m map[string]any) { delete(m, "version") }},
		{"both repo and org", 4, func(m map[string]any) { m["org"] = "tonitienda" }},
		{"neither repo nor org", 4, func(m map[string]any) { delete(m, "repository") }},
		{"triggers empty", 5, func(m map[string]any) { m["triggers"] = []any{} }},
		{"cron without timezone", 5, func(m map[string]any) {
			m["triggers"] = []any{map[string]any{"cron": map[string]any{"schedule": "0 3 * * *"}}}
		}},
		{"cron bare offset", 5, func(m map[string]any) {
			m["triggers"] = []any{map[string]any{"cron": map[string]any{"schedule": "0 3 * * *", "timezone": "+02:00"}}}
		}},
		{"followup without max_runs", 5, func(m map[string]any) {
			m["triggers"] = []any{map[string]any{"followup": map[string]any{"of": "implement"}}}
		}},
		{"concurrency limit zero", 6, func(m map[string]any) {
			m["concurrency"].(map[string]any)["limit"] = 0.0
		}},
		{"concurrency limit missing", 6, func(m map[string]any) {
			delete(m["concurrency"].(map[string]any), "limit")
		}},
		{"budget run missing", 7, func(m map[string]any) {
			delete(m["budget"].(map[string]any), "run")
		}},
		{"step budgets exceed run", 7, func(m map[string]any) {
			m["budget"].(map[string]any)["run"] = 1.0
		}},
		{"unknown label", 8, func(m map[string]any) {
			m["steps"].([]any)[3].(map[string]any)["with"] = map[string]any{"label": "ghost"}
		}},
		{"undeclared secret scope", 9, func(m map[string]any) {
			m["secrets"] = []any{"github-token"} // drop anthropic-api-key needed by agent step
		}},
		{"provider_policy no routing", 10, func(m map[string]any) {
			m["steps"].([]any)[0].(map[string]any)["provider_policy"] = "nope"
		}},
		{"agent step missing role", 11, func(m map[string]any) {
			delete(m["steps"].([]any)[0].(map[string]any), "role")
		}},
		{"github step with role", 11, func(m map[string]any) {
			m["steps"].([]any)[2].(map[string]any)["role"] = "deploy"
		}},
		{"merge enabled without policy", 12, func(m map[string]any) { delete(m, "merge_policy") }},
		{"merge permits forbidden invariant", 12, func(m map[string]any) {
			req := m["merge_policy"].(map[string]any)["required"].([]any)
			m["merge_policy"].(map[string]any)["required"] = append(req, map[string]any{"force_push": true})
		}},
		{"unknown merge predicate", 12, func(m map[string]any) {
			req := m["merge_policy"].(map[string]any)["required"].([]any)
			m["merge_policy"].(map[string]any)["required"] = append(req, map[string]any{"phase_of_moon": true})
		}},
		{"when with operator", 13, func(m map[string]any) {
			m["steps"].([]any)[4].(map[string]any)["when"] = "policy.a && policy.b"
		}},
		{"when unknown namespace", 13, func(m map[string]any) {
			m["steps"].([]any)[4].(map[string]any)["when"] = "weather.sunny"
		}},
		{"unknown interpolation var", 13, func(m map[string]any) {
			m["concurrency"].(map[string]any)["key"] = "k:${mystery}"
		}},
		{"plaintext secret in with", 14, func(m map[string]any) {
			m["steps"].([]any)[3].(map[string]any)["with"] = map[string]any{"token": "ghp_abcdefghijklmnopqrstuvwxyz0123"}
		}},
		{"secrets ref unknown scope", 14, func(m map[string]any) {
			m["concurrency"].(map[string]any)["key"] = "k:${secrets.unlisted}"
		}},
		{"duration bad grammar", 16, func(m map[string]any) { m["timeout"] = "1h30m" }},
		{"duration bare int", 16, func(m map[string]any) { m["timeout"] = "45" }},
		{"merge predicate not single-key map", 17, func(m map[string]any) {
			m["merge_policy"].(map[string]any)["required"] = []any{
				map[string]any{"pr_author_is_smith": true, "extra": true},
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := Load("job.yaml", mutate(t, tc.fn))
			if !hasRule(errs, tc.rule) {
				t.Fatalf("want a rule-%d error, got %v", tc.rule, errs)
			}
		})
	}
}

// TestMultiTriggerInputCrossCheck covers rule 15: a ${trigger.inputs.X} on a
// multi-trigger job is rejected unless every trigger declares X.
func TestMultiTriggerInputCrossCheck(t *testing.T) {
	m := mutate(t, func(m map[string]any) {
		m["triggers"] = []any{
			map[string]any{"manual": map[string]any{"inputs": map[string]any{"scenario": map[string]any{"type": "string"}}}},
			map[string]any{"github.pr_merged": map[string]any{}},
		}
		m["concurrency"].(map[string]any)["key"] = "k:${trigger.inputs.scenario}"
	})
	if _, errs := Load("job.yaml", m); !hasRule(errs, 15) {
		t.Fatalf("want rule-15 error for input not declared by all triggers, got %v", errs)
	}

	// Single manual trigger declaring the input is accepted.
	ok := mutate(t, func(m map[string]any) {
		m["triggers"] = []any{
			map[string]any{"manual": map[string]any{"inputs": map[string]any{"scenario": map[string]any{"type": "string"}}}},
		}
		m["concurrency"].(map[string]any)["key"] = "k:${trigger.inputs.scenario}"
	})
	if _, errs := Load("job.yaml", ok); hasRule(errs, 15) {
		t.Fatalf("single-trigger job declaring the input should pass rule 15: %v", errs)
	}
}

// TestCheckUnique covers rule 2's cross-spec id-collision clause.
func TestCheckUnique(t *testing.T) {
	a, _ := Load("a.yaml", decode(t, validImplement))
	b, _ := Load("b.yaml", decode(t, validImplement)) // same id
	errs := CheckUnique([]*Spec{a, b})
	if !hasRule(errs, 2) {
		t.Fatalf("want rule-2 collision error, got %v", errs)
	}
	if errs := CheckUnique([]*Spec{a}); len(errs) != 0 {
		t.Fatalf("single spec must be unique: %v", errs)
	}
}

func TestParseDuration(t *testing.T) {
	good := map[string]float64{"30s": 30, "45m": 45 * 60, "2h": 2 * 3600, "90d": 90 * 86400}
	for in, wantSec := range good {
		d, err := ParseDuration(in)
		if err != nil {
			t.Fatalf("ParseDuration(%q) errored: %v", in, err)
		}
		if d.Std().Seconds() != wantSec {
			t.Fatalf("ParseDuration(%q) = %v, want %v s", in, d, wantSec)
		}
	}
	for _, bad := range []string{"", "45", "1h30m", "1.5h", "-5m", "5y", "h", "5 m"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Fatalf("ParseDuration(%q) should fail", bad)
		}
	}
}

// TestHookRejectsAgentAction guards §4.9: hooks are bookkeeping, not cognition.
func TestHookRejectsAgentAction(t *testing.T) {
	m := mutate(t, func(m map[string]any) {
		m["hooks"] = map[string]any{
			"on_success": []any{map[string]any{"uses": "agent.review", "role": "review"}},
		}
	})
	if _, errs := Load("job.yaml", m); !hasRule(errs, 1) {
		t.Fatalf("agent action in a hook must be rejected: %v", errs)
	}
}
