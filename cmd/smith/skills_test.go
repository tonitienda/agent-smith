package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
)

func TestSkillToolReturnsBodyAttributed(t *testing.T) {
	skills := []skill.Skill{{Name: "research", Description: "do research", Body: "Follow these steps."}}
	tl := skillTool(skills)

	if d := tl.Def(); !strings.Contains(d.Description, "research: do research") {
		t.Errorf("description missing skill catalog: %q", d.Description)
	}

	out, err := tl.Run(context.Background(), json.RawMessage(`{"name":"research"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.IsError || out.Text != "Follow these steps." {
		t.Errorf("unexpected output: %+v", out)
	}
	if out.Attribution == nil || out.Attribution.Skill != "research" {
		t.Errorf("output not attributed to skill: %+v", out.Attribution)
	}
}

func TestSkillToolUnknownNameIsModelError(t *testing.T) {
	tl := skillTool([]skill.Skill{{Name: "research"}})
	out, err := tl.Run(context.Background(), json.RawMessage(`{"name":"missing"}`))
	if err != nil {
		t.Fatalf("Run returned infra error: %v", err)
	}
	if !out.IsError || !strings.Contains(out.Text, "unknown skill") {
		t.Errorf("want model-readable unknown-skill error, got %+v", out)
	}
}

func loadedSkillNames(sess *session.Session) []string {
	var names []string
	for _, b := range sess.Log.Events() {
		if b.Kind == eventlog.KindSkillLoad && b.Attribution != nil {
			names = append(names, b.Attribution.Skill)
		}
	}
	return names
}

func TestSeedSkillsRecordsLoadEvents(t *testing.T) {
	skills := []skill.Skill{{Name: "beta"}, {Name: "alpha"}}

	store, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Log.Close() }()

	if err := seedSkills(sess, skills); err != nil {
		t.Fatalf("seedSkills: %v", err)
	}

	names := loadedSkillNames(sess)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("skill-load events = %v, want [alpha beta]", names)
	}
}

// TestSeedSkillsReconcilesWithoutDuplicating covers the resume/clear path: a
// second seed against the same snapshot must not re-append events already on the
// log, but must add an event for a newly available skill.
func TestSeedSkillsReconcilesWithoutDuplicating(t *testing.T) {
	store, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Log.Close() }()

	if err := seedSkills(sess, []skill.Skill{{Name: "alpha"}}); err != nil {
		t.Fatalf("seedSkills: %v", err)
	}
	// Re-seed with alpha (already logged) plus a newly available beta.
	if err := seedSkills(sess, []skill.Skill{{Name: "alpha"}, {Name: "beta"}}); err != nil {
		t.Fatalf("seedSkills re-seed: %v", err)
	}

	names := loadedSkillNames(sess)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("skill-load events = %v, want [alpha beta] (alpha not duplicated)", names)
	}
}
