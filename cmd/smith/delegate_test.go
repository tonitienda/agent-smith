package main

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/skill"
)

// TestChildToolsExcludesTask verifies a delegated child's registry carries the
// builtin tool set but never the `task` tool, so delegation cannot recurse
// (AS-119). With no skills it also offers no `skill` tool.
func TestChildToolsExcludesTask(t *testing.T) {
	reg, err := childTools(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("childTools: %v", err)
	}
	for _, name := range []string{"read", "write", "edit", "glob", "grep", "shell"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("child registry missing builtin tool %q", name)
		}
	}
	if _, ok := reg.Get("task"); ok {
		t.Error("child registry must not include the task tool (no recursion)")
	}
	if _, ok := reg.Get("skill"); ok {
		t.Error("child registry must not include the skill tool when no skills are present")
	}
}

// TestChildToolsInheritsSkills verifies a child inherits the parent's skills
// (AS-034) via the skill tool while still excluding `task` (AS-119).
func TestChildToolsInheritsSkills(t *testing.T) {
	skills := []skill.Skill{{Name: "demo", Description: "a demo skill", Scope: "project", Body: "do the thing"}}
	reg, err := childTools(t.TempDir(), skills, nil)
	if err != nil {
		t.Fatalf("childTools: %v", err)
	}
	if _, ok := reg.Get("skill"); !ok {
		t.Error("child registry must include the skill tool when the parent has skills")
	}
	if _, ok := reg.Get("task"); ok {
		t.Error("child registry must not include the task tool (no recursion)")
	}
}
