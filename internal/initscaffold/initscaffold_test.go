package initscaffold

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// changeFor returns the planned change for the given relative path, or fails.
func changeFor(t *testing.T, p Plan, rel string) FileChange {
	t.Helper()
	for _, c := range p.Changes {
		if c.Rel == rel {
			return c
		}
	}
	t.Fatalf("no planned change for %q; changes=%v", rel, relPaths(p))
	return FileChange{}
}

func relPaths(p Plan) []string {
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Rel)
	}
	return out
}

// AS-039 AC1: on a Go repo with a Makefile the memory file names the project's
// own build/test/lint commands (Makefile targets win over language defaults).
func TestScanGoRepoNamesMakeTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")
	write(t, dir, "Makefile", "build:\n\tgo build ./...\ntest:\n\tgo test ./...\nlint:\n\tgolangci-lint run\n")
	write(t, dir, "cmd/x/main.go", "package main\n")
	write(t, dir, "internal/y/y.go", "package y\n")

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := changeFor(t, plan, "AGENT.md")
	for _, want := range []string{"`make build`", "`make test`", "`make lint`", "`cmd/`", "`internal/`"} {
		if !strings.Contains(agent.NewContent, want) {
			t.Errorf("AGENT.md missing %q\n%s", want, agent.NewContent)
		}
	}
	if !agent.Created() {
		t.Error("AGENT.md should be a creation")
	}
}

// The Makefile parser ignores variable assignments and indented colon lines,
// so they are never mistaken for targets.
func TestMakeTargetsIgnoresNonTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", strings.Join([]string{
		"VAR := value",
		"OTHER ::= x",
		"\t# note: recipe comment",
		"  indented: not-a-target",
		"build: VAR",
		"\tgo build ./...",
		"test:",
		"\tgo test ./...",
	}, "\n"))

	got := makeTargets(os.DirFS(dir))
	for _, bad := range []string{"VAR", "OTHER", "indented"} {
		if got[bad] {
			t.Errorf("parsed non-target %q as a target: %v", bad, got)
		}
	}
	for _, want := range []string{"build", "test"} {
		if !got[want] {
			t.Errorf("missed target %q: %v", want, got)
		}
	}
}

// AS-039 AC1: a Go repo without a Makefile falls back to the go toolchain.
func TestScanGoRepoFallsBackToGoToolchain(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n")

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := changeFor(t, plan, "AGENT.md")
	for _, want := range []string{"`go build ./...`", "`go test ./...`", "`go vet ./...`"} {
		if !strings.Contains(agent.NewContent, want) {
			t.Errorf("AGENT.md missing %q\n%s", want, agent.NewContent)
		}
	}
}

// AS-039 AC1: a JS repo names its package.json scripts.
func TestScanJSRepoNamesScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"build":"tsc","test":"jest","lint":"eslint ."}}`)
	write(t, dir, "src/index.js", "")

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := changeFor(t, plan, "AGENT.md")
	for _, want := range []string{"`npm run build`", "`npm test`", "`npm run lint`", "`src/`"} {
		if !strings.Contains(agent.NewContent, want) {
			t.Errorf("AGENT.md missing %q\n%s", want, agent.NewContent)
		}
	}
}

// AS-039 AC2: an existing CLAUDE.md is amended (its content preserved) rather
// than overwritten or shadowed by a competing AGENT.md.
func TestScanAmendsExistingMemoryFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n")
	write(t, dir, "CLAUDE.md", "# House rules\n\nAlways be kind.\n")

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range plan.Changes {
		if c.Rel == "AGENT.md" {
			t.Fatalf("must not create a competing AGENT.md beside CLAUDE.md")
		}
	}
	c := changeFor(t, plan, "CLAUDE.md")
	if !strings.HasPrefix(c.NewContent, "# House rules\n\nAlways be kind.\n") {
		t.Errorf("existing content not preserved:\n%s", c.NewContent)
	}
	if !strings.Contains(c.NewContent, "## Build & test") {
		t.Errorf("missing sections not appended:\n%s", c.NewContent)
	}
	if c.Created() {
		t.Error("amend should not report as a creation")
	}
}

// AS-039 AC3: re-running on an initialized project proposes only deltas — once
// everything is written, a second scan plans nothing.
func TestScanReRunIsNoOp(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n")
	write(t, dir, "Makefile", "build:\n\tgo build ./...\ntest:\n\tgo test ./...\n")

	first, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Empty() {
		t.Fatal("first scan should propose changes")
	}
	if err := first.Apply(); err != nil {
		t.Fatal(err)
	}

	second, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Empty() {
		t.Errorf("re-run proposed changes on an initialized project: %v", relPaths(second))
	}
}

// AS-039 AC2/AC3: a memory file already carrying a section keeps it; only the
// genuinely missing section is appended (a delta, not a rewrite).
func TestScanAmendsOnlyMissingSections(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n")
	write(t, dir, "cmd/x/main.go", "package main\n")
	// Pre-existing AGENT.md already documents Build & test but not Layout.
	write(t, dir, "AGENT.md", "# x\n\n## Build & test\n- Build: `custom`\n")

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := changeFor(t, plan, "AGENT.md")
	if strings.Contains(c.added(), "## Build & test") {
		t.Errorf("re-proposed an existing section:\n%s", c.added())
	}
	if !strings.Contains(c.added(), "## Layout") {
		t.Errorf("missing Layout section not appended:\n%s", c.added())
	}
	// The user's custom build command must survive untouched.
	if !strings.Contains(c.NewContent, "- Build: `custom`") {
		t.Errorf("user content clobbered:\n%s", c.NewContent)
	}
}

// AS-039: the .agent-smith/ scaffold is created and Apply writes it to disk,
// and the AGENT.md uses a recognized memory filename so it is picked up by the
// loader next session (AC4).
func TestApplyWritesScaffold(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n")

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	changeFor(t, plan, filepath.Join(".agent-smith", "config.json"))
	changeFor(t, plan, filepath.Join(".agent-smith", "commands", "README.md"))

	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"AGENT.md",
		filepath.Join(".agent-smith", "config.json"),
		filepath.Join(".agent-smith", "commands", "README.md"),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %s on disk: %v", rel, err)
		}
	}
}

// The inspection helpers read through fs.FS, so they can be driven from an
// in-memory tree with no disk I/O — Makefile targets, layout dirs, and the
// existing memory file are all discovered from fstest.MapFS.
func TestInspectionHelpersOverMapFS(t *testing.T) {
	fsys := fstest.MapFS{
		"Makefile":      {Data: []byte("build:\n\tgo build ./...\ntest:\n\tgo test ./...\n")},
		"go.mod":        {Data: []byte("module x\n")},
		"cmd/x/main.go": {Data: []byte("package main\n")},
		"internal/y.go": {Data: []byte("package y\n")},
		"CLAUDE.md":     {Data: []byte("# rules\n")},
	}
	if got := makeTargets(fsys); !got["build"] || !got["test"] {
		t.Errorf("makeTargets = %v, want build+test", got)
	}
	build, test, lint := commands(fsys)
	if build != "make build" || test != "make test" || lint != "go vet ./..." {
		t.Errorf("commands = %q/%q/%q", build, test, lint)
	}
	if got := layout(fsys); strings.Join(got, ",") != "cmd,internal" {
		t.Errorf("layout = %v, want [cmd internal]", got)
	}
	path, content, err := existingMemoryFile(fsys, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join("/proj", "CLAUDE.md") || content != "# rules\n" {
		t.Errorf("existingMemoryFile = %q, %q", path, content)
	}
}

// An empty plan renders an explanatory no-op message rather than a diff.
func TestRenderEmptyPlan(t *testing.T) {
	var p Plan
	p.Skipped = append(p.Skipped, "AGENT.md already covers build/test and layout")
	out := p.Render()
	if !strings.Contains(out, "already set up") {
		t.Errorf("unexpected empty render:\n%s", out)
	}
}

// fakeEnricher records the facts it was handed and returns a fixed result, so the
// enrichment plumbing (AS-087) can be tested without a model.
type fakeEnricher struct {
	got  Facts
	secs []ProseSection
	err  error
}

func (f *fakeEnricher) Enrich(_ context.Context, facts Facts) ([]ProseSection, error) {
	f.got = facts
	return f.secs, f.err
}

// AS-087: --describe appends model-authored prose to the created memory file
// after the deterministic sections, and the enricher is handed the deterministic
// scan facts (commands, layout, README sample) to ground its prose.
func TestScanWithEnrichmentAppendsProse(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")
	write(t, dir, "cmd/x/main.go", "package main\n")
	write(t, dir, "README.md", "# X\n\nX is a widget service.\n")

	e := &fakeEnricher{secs: []ProseSection{{Title: "Overview", Body: "X is a widget service."}}}
	plan, err := ScanWithEnrichment(context.Background(), dir, e)
	if err != nil {
		t.Fatal(err)
	}

	if e.got.ProjectName != filepath.Base(dir) {
		t.Errorf("facts.ProjectName = %q, want %q", e.got.ProjectName, filepath.Base(dir))
	}
	if e.got.Test != "go test ./..." {
		t.Errorf("facts.Test = %q, want deterministic go test", e.got.Test)
	}
	if !strings.Contains(e.got.Readme, "widget service") {
		t.Errorf("facts.Readme missing README sample: %q", e.got.Readme)
	}

	agent := changeFor(t, plan, "AGENT.md")
	// Deterministic section still present and not replaced.
	if !strings.Contains(agent.NewContent, "`go test ./...`") {
		t.Errorf("deterministic build/test section dropped:\n%s", agent.NewContent)
	}
	// Prose section appended after the deterministic ones.
	if !strings.Contains(agent.NewContent, "## Overview") || !strings.Contains(agent.NewContent, "X is a widget service.") {
		t.Errorf("prose section not appended:\n%s", agent.NewContent)
	}
	if idx, pidx := strings.Index(agent.NewContent, "## Build & test"), strings.Index(agent.NewContent, "## Overview"); idx < 0 || pidx < idx {
		t.Errorf("prose should follow deterministic sections:\n%s", agent.NewContent)
	}
}

// A nil enricher makes ScanWithEnrichment identical to Scan (deterministic only).
func TestScanWithEnrichmentNilIsPlainScan(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")

	got, err := ScanWithEnrichment(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changeFor(t, got, "AGENT.md").NewContent != changeFor(t, want, "AGENT.md").NewContent {
		t.Error("nil enricher should match plain Scan")
	}
}

// An enricher error fails the scan so the caller can fall back rather than
// silently dropping the requested prose.
func TestScanWithEnrichmentPropagatesError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")

	e := &fakeEnricher{err: errors.New("boom")}
	if _, err := ScanWithEnrichment(context.Background(), dir, e); err == nil {
		t.Fatal("want error from failing enricher, got nil")
	}
}

// Empty or whitespace-only prose sections are dropped, never written as blank
// headings.
func TestScanWithEnrichmentSkipsEmptySections(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")

	e := &fakeEnricher{secs: []ProseSection{{Title: "", Body: "no title"}, {Title: "Empty", Body: "  \n"}}}
	plan, err := ScanWithEnrichment(context.Background(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(changeFor(t, plan, "AGENT.md").NewContent, "## Empty") {
		t.Error("empty-body section should be dropped")
	}
}
