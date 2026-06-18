package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/skill"
)

func writeProjectSkill(t *testing.T, wd, name, content string) {
	t.Helper()
	sd := filepath.Join(skill.ProjectDir(wd), name)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

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

func TestSeedSkillsRecordsLoadEvents(t *testing.T) {
	wd := t.TempDir()
	writeProjectSkill(t, wd, "beta", "---\nname: beta\n---\nbody")
	writeProjectSkill(t, wd, "alpha", "---\nname: alpha\n---\nbody")

	store, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Log.Close() }()

	if err := seedSkills(wd, sess); err != nil {
		t.Fatalf("seedSkills: %v", err)
	}

	var names []string
	for _, b := range sess.Log.Events() {
		if b.Kind == eventlog.KindSkillLoad {
			if b.Attribution == nil {
				t.Fatalf("skill-load event has no attribution: %+v", b)
			}
			names = append(names, b.Attribution.Skill)
		}
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("skill-load events = %v, want [alpha beta]", names)
	}
}
