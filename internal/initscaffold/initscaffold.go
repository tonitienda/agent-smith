// Package initscaffold backs the /init command (AS-039): it inspects a project
// and proposes the files that bootstrap it for Agent Smith — a memory file
// (AGENT.md, our canonical name; AS-032 treats AGENT/AGENTS/CLAUDE.md as
// equivalent) describing how to build/test/lint the repo, plus a .agent-smith/
// scaffold (a config stub and a custom-commands directory, AS-031/AS-033).
//
// The scan is deterministic rather than model-assisted: build/test/lint commands
// are read straight from the project's own Makefile targets and package.json
// scripts, which names them exactly and keeps the result testable and free of
// token cost. (Model-assisted prose enrichment is deferred — see AS-087.)
//
// Nothing is written by Scan; it returns a Plan the caller previews (Render) and
// only then commits (Apply), so /init never clobbers. An existing memory file is
// amended with the sections it is missing rather than overwritten, and a
// re-run on an already-initialized project proposes only the deltas (often
// nothing), satisfying AS-039's "never clobber / only deltas" acceptance bar.
package initscaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/memory"
)

// maxDiffLines caps how many added lines the preview prints per file, so a large
// generated memory file or scaffold does not flood the screen.
const maxDiffLines = 60

// configStub is the .agent-smith/config.json written when none exists. It is a
// valid empty layered-config document (AS-031): the project layer overrides the
// user and default layers, and an empty object simply inherits everything.
const configStub = "{}\n"

// commandsReadme documents the custom-command directory (AS-033) the scaffold
// creates, since JSON config carries no comments of its own.
const commandsReadme = `# Custom commands

Drop a Markdown file here to define a project-local slash command (AS-033).
The file name (without ` + "`.md`" + `) is the command name; its body is the
prompt template. These commands are available in the TUI and as ` + "`smith`" + `
subcommands for everyone working in this repository.
`

// FileChange is one proposed write: either a new file or an amendment that
// appends to an existing one. OldContent is empty for a creation.
type FileChange struct {
	Path       string // absolute path that will be written
	Rel        string // path relative to the project root, for display
	OldContent string // current content ("" when creating)
	NewContent string // content to write
}

// Created reports whether this change creates a new file rather than amending one.
func (c FileChange) Created() bool { return c.OldContent == "" }

// added returns the text NewContent adds on top of OldContent. Amendments are
// built as OldContent followed by the new sections, so the delta is the suffix;
// a creation's delta is the whole file.
func (c FileChange) added() string { return strings.TrimPrefix(c.NewContent, c.OldContent) }

// Plan is the set of changes /init proposes for a project, plus human-readable
// notes about anything already up to date (so a no-op re-run still explains
// itself).
type Plan struct {
	Changes []FileChange
	Skipped []string
}

// Empty reports whether the plan would write nothing.
func (p Plan) Empty() bool { return len(p.Changes) == 0 }

// Scan inspects wd and builds the proposed plan without touching the filesystem.
// Reads go through an fs.FS rooted at wd (os.DirFS), so the inspection is bounded
// to the project tree and testable with an in-memory filesystem.
func Scan(wd string) (Plan, error) {
	abs, err := filepath.Abs(wd)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve working dir: %w", err)
	}
	fsys := os.DirFS(abs)

	var p Plan
	if err := p.planMemory(fsys, abs); err != nil {
		return Plan{}, err
	}
	p.planScaffold(fsys, abs)
	return p, nil
}

// planMemory proposes the memory file: it amends an existing project-level
// AGENT/AGENTS/CLAUDE.md with any missing sections, or creates AGENT.md.
func (p *Plan) planMemory(fsys fs.FS, wd string) error {
	sections := memorySections(fsys)

	target, existing, err := existingMemoryFile(fsys, wd)
	if err != nil {
		return err
	}
	if target == "" {
		// No memory file yet — create our canonical AGENT.md from every section.
		var b strings.Builder
		b.WriteString("# " + filepath.Base(wd) + " — agent guide\n")
		for _, s := range sections {
			b.WriteString("\n" + s.render())
		}
		p.add(wd, filepath.Join(wd, "AGENT.md"), "", b.String())
		return nil
	}

	// A memory file exists — append only the sections whose heading it lacks, so
	// we never clobber the user's own guidance and a re-run is a no-op.
	var missing []section
	for _, s := range sections {
		if !strings.Contains(existing, s.heading()) {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		p.Skipped = append(p.Skipped, rel(wd, target)+" already covers build/test and layout")
		return nil
	}
	content := strings.TrimRight(existing, "\n") + "\n"
	for _, s := range missing {
		content += "\n" + s.render()
	}
	p.add(wd, target, existing, content)
	return nil
}

// planScaffold proposes the .agent-smith/ files that do not yet exist.
func (p *Plan) planScaffold(fsys fs.FS, wd string) {
	files := []struct{ rel, content string }{
		{".agent-smith/config.json", configStub},
		{".agent-smith/commands/README.md", commandsReadme},
	}
	for _, f := range files {
		abs := filepath.Join(wd, filepath.FromSlash(f.rel))
		if _, err := fs.Stat(fsys, f.rel); err == nil {
			p.Skipped = append(p.Skipped, rel(wd, abs)+" already exists")
			continue
		}
		p.add(wd, abs, "", f.content)
	}
}

func (p *Plan) add(wd, path, old, new string) {
	p.Changes = append(p.Changes, FileChange{
		Path:       path,
		Rel:        rel(wd, path),
		OldContent: old,
		NewContent: new,
	})
}

// Render returns the preview text: a per-file diff of the additions, capped so a
// large file does not overwhelm the screen.
func (p Plan) Render() string {
	if p.Empty() {
		var b strings.Builder
		b.WriteString("Nothing to do — the project is already set up for Agent Smith.\n")
		for _, s := range p.Skipped {
			b.WriteString("  · " + s + "\n")
		}
		return b.String()
	}

	var b strings.Builder
	b.WriteString("/init will write the following. Nothing is saved until you confirm.\n")
	for _, c := range p.Changes {
		verb := "amend"
		if c.Created() {
			verb = "create"
		}
		fmt.Fprintf(&b, "\n%s %s\n", verb, c.Rel)
		lines := strings.Split(strings.TrimRight(c.added(), "\n"), "\n")
		for i, ln := range lines {
			if i == maxDiffLines {
				fmt.Fprintf(&b, "  … (%d more lines)\n", len(lines)-maxDiffLines)
				break
			}
			b.WriteString("  + " + ln + "\n")
		}
	}
	for _, s := range p.Skipped {
		b.WriteString("\nskip " + s + "\n")
	}
	b.WriteString("\nApply with /init --apply, or discard with /init --cancel.\n")
	return b.String()
}

// Apply writes every change to disk, creating parent directories as needed. Each
// file is written atomically (temp file + rename) so an interrupted run cannot
// leave a half-written memory file behind.
func (p Plan) Apply() error {
	for _, c := range p.Changes {
		if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", c.Rel, err)
		}
		if err := atomicWrite(c.Path, []byte(c.NewContent)); err != nil {
			return fmt.Errorf("write %s: %w", c.Rel, err)
		}
	}
	return nil
}

// section is one block of the generated memory file, identified by its heading so
// an amend can tell which sections an existing file already carries.
type section struct {
	title string
	body  string
}

func (s section) heading() string { return "## " + s.title }
func (s section) render() string  { return s.heading() + "\n" + s.body }

// memorySections builds the memory-file sections from a deterministic scan of fsys.
func memorySections(fsys fs.FS) []section {
	var out []section

	build, test, lint := commands(fsys)
	if build != "" || test != "" || lint != "" {
		var b strings.Builder
		if build != "" {
			b.WriteString("- Build: `" + build + "`\n")
		}
		if test != "" {
			b.WriteString("- Test: `" + test + "`\n")
		}
		if lint != "" {
			b.WriteString("- Lint: `" + lint + "`\n")
		}
		out = append(out, section{title: "Build & test", body: b.String()})
	}

	if dirs := layout(fsys); len(dirs) > 0 {
		var b strings.Builder
		for _, d := range dirs {
			b.WriteString("- `" + d + "/`\n")
		}
		out = append(out, section{title: "Layout", body: b.String()})
	}
	return out
}

// commands returns the project's build/test/lint commands. Makefile targets win
// (they name the project's own entry points exactly); otherwise language
// defaults are used based on the manifest present. Any of the three may be "".
func commands(fsys fs.FS) (build, test, lint string) {
	targets := makeTargets(fsys)
	mk := func(target, fallback string) string {
		if targets[target] {
			return "make " + target
		}
		return fallback
	}

	var db, dt, dl string
	switch {
	case fileExists(fsys, "go.mod"):
		db, dt, dl = "go build ./...", "go test ./...", "go vet ./..."
	case fileExists(fsys, "package.json"):
		s := packageScripts(fsys)
		if s["build"] {
			db = "npm run build"
		}
		if s["test"] {
			dt = "npm test"
		}
		if s["lint"] {
			dl = "npm run lint"
		}
	}
	return mk("build", db), mk("test", dt), mk("lint", dl)
}

// makeTargets returns the set of top-level targets declared in the project's Makefile.
func makeTargets(fsys fs.FS) map[string]bool {
	data, err := fs.ReadFile(fsys, "Makefile")
	if err != nil {
		return nil
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		// A target line starts at column 0 (recipes are indented) and reads
		// "name:" possibly with prerequisites after the colon. Skip indented
		// lines, comments, and special targets (.PHONY etc.).
		if line == "" || line[0] == '\t' || line[0] == ' ' || line[0] == '#' || line[0] == '.' {
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		// A colon that is part of an assignment operator (name := or ::=) marks a
		// variable, not a target: after the first colon the remainder begins with
		// "=" (:=) or ":=" (::=). A double-colon rule ("target:: deps") instead
		// has a space or prerequisite there, so it is kept.
		if t := strings.TrimSpace(rest); strings.HasPrefix(t, "=") || strings.HasPrefix(t, ":=") {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" && !strings.ContainsAny(name, " \t=") {
			targets[name] = true
		}
	}
	return targets
}

// packageScripts returns the set of script names defined in the project's package.json.
func packageScripts(fsys fs.FS) map[string]bool {
	data, err := fs.ReadFile(fsys, "package.json")
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	out := map[string]bool{}
	for name := range pkg.Scripts {
		out[name] = true
	}
	return out
}

// layout returns the conventional source directories present in fsys, in a stable
// order, so the memory file points a newcomer at where the code lives.
func layout(fsys fs.FS) []string {
	candidates := []string{"cmd", "internal", "pkg", "src", "lib", "app"}
	var out []string
	for _, d := range candidates {
		if fi, err := fs.Stat(fsys, d); err == nil && fi.IsDir() {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// existingMemoryFile returns the project-level memory file to amend (its absolute
// path, rooted at wd, and content), honoring the AS-032 filename precedence, or
// "" when none exists.
func existingMemoryFile(fsys fs.FS, wd string) (path, content string, err error) {
	for _, name := range memory.Filenames {
		data, readErr := fs.ReadFile(fsys, name)
		switch {
		case readErr == nil:
			return filepath.Join(wd, name), string(data), nil
		case errors.Is(readErr, fs.ErrNotExist):
			continue
		default:
			return "", "", fmt.Errorf("read %s: %w", name, readErr)
		}
	}
	return "", "", nil
}

func fileExists(fsys fs.FS, name string) bool {
	fi, err := fs.Stat(fsys, name)
	return err == nil && !fi.IsDir()
}

func rel(wd, path string) string {
	if r, err := filepath.Rel(wd, path); err == nil {
		return r
	}
	return path
}

// atomicWrite writes data to path via a temp file and rename, so a reader never
// sees a partially written file.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".init-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
