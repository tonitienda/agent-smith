package skill

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSkill drops a SKILL.md with content into dir/<name>/, creating dirs as
// needed.
func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	sd := filepath.Join(dir, name)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sd, err)
	}
	if err := os.WriteFile(filepath.Join(sd, fileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestParseFrontmatter(t *testing.T) {
	name, desc, meta, body := parseFrontmatter("---\nname: deep-research\ndescription: Research a topic\nexpected_outcome: a report\n---\nDo the research.\n")
	if name != "deep-research" {
		t.Errorf("name = %q, want deep-research", name)
	}
	if desc != "Research a topic" {
		t.Errorf("desc = %q", desc)
	}
	if got := meta["expected_outcome"]; got != "a report" {
		t.Errorf("meta[expected_outcome] = %q, want preserved", got)
	}
	if body != "Do the research.\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatterNoFence(t *testing.T) {
	name, desc, _, body := parseFrontmatter("Just instructions, no frontmatter")
	if name != "" || desc != "" {
		t.Errorf("expected empty name/desc, got %q/%q", name, desc)
	}
	if body != "Just instructions, no frontmatter" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatterCRLF(t *testing.T) {
	name, _, _, body := parseFrontmatter("---\r\nname: x\r\n---\r\nbody\r\n")
	if name != "x" {
		t.Errorf("name = %q, want x (CRLF should be normalized)", name)
	}
	if body != "body\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatterNoTrailingNewline(t *testing.T) {
	// A file ending exactly at the closing fence (no trailing newline) must still
	// parse its frontmatter rather than swallowing it into the body.
	name, desc, _, body := parseFrontmatter("---\nname: x\ndescription: d\n---")
	if name != "x" || desc != "d" {
		t.Errorf("frontmatter not parsed: name=%q desc=%q", name, desc)
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestLoadDiscoversAndSorts(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "beta", "---\nname: beta\ndescription: B\n---\nbeta body")
	writeSkill(t, dir, "alpha", "---\nname: alpha\ndescription: A\n---\nalpha body")
	// A directory without a SKILL.md is not a skill.
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	skills, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %+v", len(skills), skills)
	}
	if skills[0].Name != "alpha" || skills[1].Name != "beta" {
		t.Errorf("not sorted by name: %q, %q", skills[0].Name, skills[1].Name)
	}
	if skills[0].Body != "alpha body\n" || skills[0].Scope != "project" {
		t.Errorf("alpha = %+v", skills[0])
	}
}

func TestLoadNameFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "no-name-skill", "---\ndescription: D\n---\nbody")
	skills, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "no-name-skill" {
		t.Fatalf("expected fallback to dir name, got %+v", skills)
	}
}

func TestLoadProjectShadowsUser(t *testing.T) {
	userDir, projDir := t.TempDir(), t.TempDir()
	writeSkill(t, userDir, "shared", "---\nname: shared\ndescription: user\n---\nuser body")
	writeSkill(t, projDir, "shared", "---\nname: shared\ndescription: project\n---\nproject body")

	skills, err := Load(userDir, projDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d, want 1 (project shadows user)", len(skills))
	}
	if skills[0].Body != "project body\n" || skills[0].Scope != "project" {
		t.Errorf("project did not win: %+v", skills[0])
	}
}

func TestLoadMissingDirsNoError(t *testing.T) {
	skills, err := Load(filepath.Join(t.TempDir(), "absent"), "")
	if err != nil {
		t.Fatalf("Load on missing dir: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("want no skills, got %d", len(skills))
	}
}
