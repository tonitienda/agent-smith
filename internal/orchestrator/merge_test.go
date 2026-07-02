package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/orchestrator"
	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

// greenFacts is a PR that satisfies every auto-merge gate: a Smith-owned head,
// all checks green, base protected, repo allows auto-merge, an independent human
// approval, and no high-risk changes. Individual tests spoil one field to prove a
// gate blocks.
func greenFacts() orchestrator.MergeFacts {
	return orchestrator.MergeFacts{
		Repository:      "acme/widgets",
		Number:          42,
		Author:          "smith-bot",
		Head:            "smith/impl/issue-42",
		Base:            "main",
		Labels:          []string{"smith-generated", "smith-auto-merge"},
		ChangedFiles:    []string{"internal/foo/foo.go", "README.md"},
		Checks:          []orchestrator.Check{{Name: "build", State: "success"}, {Name: "lint", State: "success"}},
		Approvals:       []string{"maintainer"},
		BranchProtected: true,
		RepoAutoMerge:   true,
	}
}

func autoPolicy() *spec.MergePolicy {
	return &spec.MergePolicy{
		Mode: "auto",
		Required: []spec.Predicate{
			{Name: "pr_author_is_smith", Arg: true},
			{Name: "required_checks_green", Arg: true},
			{Name: "label_present", Arg: "smith-auto-merge"},
		},
		Forbidden: []spec.Predicate{
			{Name: "unknown_checks", Arg: true},
			{Name: "branch_protection_bypass", Arg: true},
			{Name: "force_push", Arg: true},
		},
	}
}

func TestEvaluateMergeAllowsGreenAutoPR(t *testing.T) {
	d := orchestrator.EvaluateMerge(autoPolicy(), greenFacts())
	if !d.Allowed {
		t.Fatalf("green PR blocked: %s", d.Reason)
	}
	// Every evaluated input is recorded for the audit trail (acceptance 5).
	for _, key := range []string{"pr", "author", "smith_authored", "checks", "approvals", "branch_protected", "repo_auto_merge", "mode"} {
		if _, ok := d.Inputs[key]; !ok {
			t.Fatalf("input %q missing from audit record: %+v", key, d.Inputs)
		}
	}
}

func TestEvaluateMergeBlocks(t *testing.T) {
	cases := []struct {
		name   string
		spoil  func(*orchestrator.MergeFacts)
		reason string // substring the recorded reason must contain
	}{
		{"repo forbids auto-merge", func(f *orchestrator.MergeFacts) { f.RepoAutoMerge = false }, "repository settings"},
		{"budget exceeded", func(f *orchestrator.MergeFacts) { f.BudgetExceeded = true }, "budget"},
		{"force pushed", func(f *orchestrator.MergeFacts) { f.ForcePushed = true }, "force-pushed"},
		{"base not protected", func(f *orchestrator.MergeFacts) { f.BranchProtected = false }, "not protected"},
		{"failing check", func(f *orchestrator.MergeFacts) { f.Checks[1].State = "failure" }, "not all green"},
		{"pending check", func(f *orchestrator.MergeFacts) { f.Checks[0].State = "pending" }, "not all green"},
		{"no checks", func(f *orchestrator.MergeFacts) { f.Checks = nil }, "missing"},
		{"workflow file changed", func(f *orchestrator.MergeFacts) {
			f.ChangedFiles = append(f.ChangedFiles, ".github/workflows/ci.yml")
		}, "high-risk"},
		{"job spec changed", func(f *orchestrator.MergeFacts) {
			f.ChangedFiles = append(f.ChangedFiles, ".agent-smith/jobs/impl.yaml")
		}, "high-risk"},
		{"secret file changed", func(f *orchestrator.MergeFacts) {
			f.ChangedFiles = append(f.ChangedFiles, "config/prod.secret.env")
		}, "high-risk"},
		{"unknown author", func(f *orchestrator.MergeFacts) { f.Author = "" }, "author is unknown"},
		{"bot-only approval", func(f *orchestrator.MergeFacts) { f.Approvals = []string{"github-actions[bot]"} }, "no independent human approval"},
		{"not smith authored", func(f *orchestrator.MergeFacts) { f.Head = "feature/human" }, "not Smith-authored"},
		{"missing required label", func(f *orchestrator.MergeFacts) { f.Labels = []string{"smith-generated"} }, "missing label"},
		{"no approval", func(f *orchestrator.MergeFacts) { f.Approvals = nil }, "no independent human approval"},
		{"self-approval only", func(f *orchestrator.MergeFacts) {
			f.Author, f.Approvals = "maintainer", []string{"maintainer"}
		}, "no independent human approval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := greenFacts()
			tc.spoil(&f)
			d := orchestrator.EvaluateMerge(autoPolicy(), f)
			if d.Allowed {
				t.Fatalf("expected block, got allow")
			}
			if !strings.Contains(d.Reason, tc.reason) {
				t.Fatalf("reason %q does not contain %q", d.Reason, tc.reason)
			}
		})
	}
}

func TestEvaluateMergeModes(t *testing.T) {
	// No policy at all is mode off.
	if d := orchestrator.EvaluateMerge(nil, greenFacts()); d.Allowed || !strings.Contains(d.Reason, "off") {
		t.Fatalf("nil policy should be off-blocked: %+v", d)
	}
	// Explicit off.
	off := &spec.MergePolicy{Mode: "off"}
	if d := orchestrator.EvaluateMerge(off, greenFacts()); d.Allowed {
		t.Fatal("mode off allowed a merge")
	}
	// Manual with no override blocks.
	manual := autoPolicy()
	manual.Mode = "manual"
	if d := orchestrator.EvaluateMerge(manual, greenFacts()); d.Allowed || !strings.Contains(d.Reason, "manual override") {
		t.Fatalf("manual/no-override should block: %+v", d)
	}
	// Manual override by the author is rejected (no self-approval).
	f := greenFacts()
	f.Author = "maintainer"
	f.Override = &orchestrator.ManualOverride{Actor: "maintainer", Reason: "ship"}
	if d := orchestrator.EvaluateMerge(manual, f); d.Allowed {
		t.Fatalf("author self-override should be rejected: %+v", d)
	}
	// Manual override by an independent human is allowed and audited.
	f.Override = &orchestrator.ManualOverride{Actor: "release-mgr", Reason: "hotfix approved offline"}
	d := orchestrator.EvaluateMerge(manual, f)
	if !d.Allowed {
		t.Fatalf("valid override blocked: %s", d.Reason)
	}
	if d.Inputs["override_actor"] != "release-mgr" || d.Inputs["override_reason"] != "hotfix approved offline" {
		t.Fatalf("override not audited: %+v", d.Inputs)
	}
}

// fakeMerge is an in-memory MergeActions port: it returns seeded facts and records
// which merge action fired so a test can exercise the wired gate offline.
type fakeMerge struct {
	facts    orchestrator.MergeFacts
	factsErr error
	acted    string // "enable_auto_merge" | "merge" | ""
}

func (f *fakeMerge) MergeFacts(context.Context, orchestrator.GitHubTarget) (orchestrator.MergeFacts, error) {
	if f.factsErr != nil {
		return orchestrator.MergeFacts{}, f.factsErr
	}
	return f.facts, nil
}
func (f *fakeMerge) EnableAutoMerge(context.Context, orchestrator.GitHubTarget) (string, error) {
	f.acted = "enable_auto_merge"
	return "https://gh/pr/42", nil
}
func (f *fakeMerge) Merge(context.Context, orchestrator.GitHubTarget) (string, error) {
	f.acted = "merge"
	return "https://gh/pr/42/merged", nil
}

func autoMergeJob() *spec.Spec {
	j := prJob()
	j.Steps = []spec.Step{{ID: "automerge", Uses: "github.enable_auto_merge"}}
	j.MergePolicy = autoPolicy()
	return j
}

// A green PR under an auto policy enables GitHub native auto-merge and records an
// approved merge decision carrying the evaluated inputs.
func TestSessionExecutorAutoMergeApproved(t *testing.T) {
	sessions := newSessionStore(t)
	mp := &fakeMerge{facts: greenFacts()}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithMergeActions(mp)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), autoMergeJob())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	if mp.acted != "enable_auto_merge" {
		t.Fatalf("auto-merge not enabled: acted=%q", mp.acted)
	}
	var approved *orchestrator.PolicyDecision
	for _, d := range policyDecisionBlocks(t, sessions) {
		if d.Policy == "merge_policy" {
			d := d
			approved = &d
		}
	}
	if approved == nil || approved.Decision != "approved" {
		t.Fatalf("no approved merge decision: %+v", approved)
	}
	if approved.Inputs["repo_auto_merge"] != "true" {
		t.Fatalf("evaluated inputs not recorded: %+v", approved.Inputs)
	}
	blocks := githubActionBlocks(t, sessions)
	if len(blocks) != 1 || blocks[0].Action != "enable_auto_merge" || blocks[0].Outcome != "ok" {
		t.Fatalf("recorded actions = %+v", blocks)
	}
}

// A PR that fails a gate is refused fail-closed: no merge fires, a blocked decision
// is recorded, and the run still succeeds (the PR simply waits for humans).
func TestSessionExecutorAutoMergeBlocked(t *testing.T) {
	sessions := newSessionStore(t)
	f := greenFacts()
	f.Checks[0].State = "pending"
	mp := &fakeMerge{facts: f}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithMergeActions(mp)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), autoMergeJob())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("blocked merge should not fail the run, got %q", out.Status)
	}
	if mp.acted != "" {
		t.Fatalf("merge fired despite block: %q", mp.acted)
	}
	var blocked bool
	for _, d := range policyDecisionBlocks(t, sessions) {
		if d.Policy == "merge_policy" && d.Decision == "blocked" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("no blocked merge decision recorded")
	}
	if len(githubActionBlocks(t, sessions)) != 0 {
		t.Fatal("recorded a merge action despite the block")
	}
}

// A merge_facts transport error fails the run closed (internal) and records the
// failed action, mirroring the PR-lifecycle failure posture.
func TestSessionExecutorMergeFactsErrorFailsRun(t *testing.T) {
	sessions := newSessionStore(t)
	mp := &fakeMerge{factsErr: errors.New("boom")}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithMergeActions(mp)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), autoMergeJob())
	if err == nil {
		t.Fatal("want error from merge_facts failure")
	}
	if out.Status != store.StatusFailed || out.FailureClass != store.FailureInternal {
		t.Fatalf("out = %+v, want failed/internal", out)
	}
}
