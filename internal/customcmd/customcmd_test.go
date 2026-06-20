package customcmd

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// write drops a command file with content into dir, creating dir as needed.
func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestExpand(t *testing.T) {
	cases := []struct {
		name string
		body string
		args []string
		want string
	}{
		{"arguments", "Fix $ARGUMENTS now", []string{"the", "bug"}, "Fix the bug now"},
		{"positional", "Compare $1 with $2", []string{"a", "b"}, "Compare a with b"},
		{"missing positional empties", "x=$1 y=$2", []string{"only"}, "x=only y="},
		{"no placeholders", "just text", []string{"ignored"}, "just text"},
		{"empty args", "all: $ARGUMENTS", nil, "all: "},
		{"out of range index", "$3", []string{"a"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expand(tc.body, tc.args); got != tc.want {
				t.Errorf("expand(%q, %v) = %q, want %q", tc.body, tc.args, got, tc.want)
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	desc, hint, body := parseFrontmatter("---\ndescription: Run the tests\nargument-hint: \"[pkg]\"\n---\nDo it for $1\n")
	if desc != "Run the tests" {
		t.Errorf("description = %q", desc)
	}
	if hint != "[pkg]" {
		t.Errorf("argument-hint = %q", hint)
	}
	if body != "Do it for $1\n" {
		t.Errorf("body = %q", body)
	}

	// No fence: whole content is the body.
	_, _, body = parseFrontmatter("plain body $ARGUMENTS")
	if body != "plain body $ARGUMENTS" {
		t.Errorf("unfenced body = %q", body)
	}

	// Unterminated fence: treated entirely as body rather than swallowing it.
	_, _, body = parseFrontmatter("---\ndescription: x\nstill going")
	if body != "---\ndescription: x\nstill going" {
		t.Errorf("unterminated body = %q", body)
	}
}

func TestLoadProjectOverridesUser(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	projDir := filepath.Join(root, "proj")

	write(t, userDir, "review.md", "---\ndescription: user review\n---\nuser body")
	write(t, userDir, "userly.md", "only at user level")
	write(t, projDir, "review.md", "---\ndescription: project review\n---\nproject $ARGUMENTS")

	cmds, err := Load(userDir, projDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2: %+v", len(cmds), cmds)
	}
	// Sorted by name: review then userly.
	review := cmds[0]
	if review.Name != "review" {
		t.Fatalf("cmds[0].Name = %q, want review", review.Name)
	}
	if review.Description != "project review" || review.Scope != "project" || !review.Overrides {
		t.Errorf("override not applied: %+v", review)
	}
	if got := review.Expand([]string{"now"}); got != "project now" {
		t.Errorf("project body won? Expand = %q", got)
	}

	userly := cmds[1]
	if userly.Name != "userly" || userly.Scope != "user" || userly.Overrides {
		t.Errorf("user-only command wrong: %+v", userly)
	}
}

func TestLoadSkipsNonMarkdownAndBadNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ok.md", "body")
	write(t, dir, "notes.txt", "ignored")
	write(t, dir, "bad name.md", "skipped: name has a space")

	cmds, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Name != "ok" {
		t.Fatalf("got %+v, want only [ok]", cmds)
	}
}

func TestLoadMissingDirsAreFine(t *testing.T) {
	cmds, err := Load("", filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Load with absent dir: %v", err)
	}
	if len(cmds) != 0 {
		t.Fatalf("got %d commands, want 0", len(cmds))
	}
}

// TestLoadFSScansInMemoryTree exercises the fs.FS scanner directly with an
// in-memory tree, covering discovery and filtering without touching disk. With no
// base the Source is the file name within the tree.
func TestLoadFSScansInMemoryTree(t *testing.T) {
	fsys := fstest.MapFS{
		"ok.md":       {Data: []byte("---\ndescription: D\n---\nbody $1")},
		"notes.txt":   {Data: []byte("ignored")},
		"bad name.md": {Data: []byte("skipped: space in name")},
	}
	cmds, err := loadFS(fsys, "", "project")
	if err != nil {
		t.Fatalf("loadFS: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Name != "ok" {
		t.Fatalf("got %+v, want only [ok]", cmds)
	}
	if cmds[0].Source != "ok.md" {
		t.Errorf("Source = %q, want ok.md", cmds[0].Source)
	}
	if cmds[0].Description != "D" || cmds[0].Expand([]string{"x"}) != "body x" {
		t.Errorf("command not parsed: %+v", cmds[0])
	}
}

func TestParseFrontmatterCRLF(t *testing.T) {
	desc, hint, body := parseFrontmatter("---\r\ndescription: Win file\r\nargument-hint: \"[x]\"\r\n---\r\nDo $1\r\n")
	if desc != "Win file" {
		t.Errorf("description = %q, want Win file", desc)
	}
	if hint != "[x]" {
		t.Errorf("argument-hint = %q, want [x]", hint)
	}
	if body != "Do $1\n" {
		t.Errorf("body = %q, want %q", body, "Do $1\n")
	}
}
