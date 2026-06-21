package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/skill"
)

// AS-108 AC1: a fact found inside a skill scope resolves to that skill's
// SKILL.md; outside one, to the deepest applicable memory file; with the
// project-root fallback (the empty return that defers to DefaultTarget) preserved.
func TestSaveTargetResolver(t *testing.T) {
	wd := t.TempDir()
	deepMem := filepath.Join(wd, "AGENT.md")
	if err := os.WriteFile(deepMem, []byte("# project memory"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := []skill.Skill{{Name: "deploy", Source: filepath.Join(wd, ".agent-smith/skills/deploy/SKILL.md")}}
	resolve := saveTargetResolver(wd, skills)

	if got := resolve("deploy", nil); got != skills[0].Source {
		t.Fatalf("skill-scoped target = %q, want %q", got, skills[0].Source)
	}
	if got := resolve("", nil); got != deepMem {
		t.Fatalf("unscoped target = %q, want deepest memory file %q", got, deepMem)
	}
	// An unknown skill falls through to the memory rule rather than guessing a path.
	if got := resolve("no-such-skill", nil); got != deepMem {
		t.Fatalf("unknown skill should fall through to memory, got %q", got)
	}
}

// With no memory file present the resolver returns "" so the detector applies its
// project-root fallback (DefaultTarget) instead of inventing a target.
func TestSaveTargetResolverFallback(t *testing.T) {
	wd := t.TempDir()
	if got := saveTargetResolver(wd, nil)("", nil); got != "" {
		t.Fatalf("no memory file should defer to the detector fallback, got %q", got)
	}
}
