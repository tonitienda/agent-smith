package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// newFS builds an FS rooted at a fresh temp dir, plus a map of files to seed.
func newFS(t *testing.T, files map[string]string, opts ...Option) *FS {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	f, err := NewFS(root, opts...)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return f
}

// byName returns the tool registered under name from the FS's tool set.
func byName(t *testing.T, f *FS, name string) tool.Tool {
	t.Helper()
	for _, tl := range f.Tools() {
		if tl.Def().Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

// run invokes a tool's Run directly with the given JSON arguments.
func run(t *testing.T, f *FS, name, args string) tool.Output {
	t.Helper()
	out, err := byName(t, f, name).Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s.Run returned a Go error: %v", name, err)
	}
	return out
}

func TestReadWholeFile(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "hello\nworld\n"})
	out := run(t, f, "read", `{"path":"a.txt"}`)

	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	if out.FileRead == nil {
		t.Fatal("read did not produce a file_read body")
	}
	if got := out.FileRead.Content; got != "hello\nworld" {
		t.Fatalf("content = %q", got)
	}
	if out.FileRead.Range != nil {
		t.Fatalf("whole-file read should have a nil range, got %+v", out.FileRead.Range)
	}
	if out.FileRead.ContentHash == "" {
		t.Fatal("missing content hash")
	}
	if out.FileRead.Path != "a.txt" {
		t.Fatalf("path = %q", out.FileRead.Path)
	}
}

func TestReadLineWindow(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "l1\nl2\nl3\nl4\nl5\n"})
	out := run(t, f, "read", `{"path":"a.txt","offset":2,"limit":2}`)

	if out.FileRead.Content != "l2\nl3" {
		t.Fatalf("content = %q", out.FileRead.Content)
	}
	r := out.FileRead.Range
	if r == nil || r.StartLine == nil || *r.StartLine != 2 || r.EndLine == nil || *r.EndLine != 3 {
		t.Fatalf("range = %+v", r)
	}
}

func TestReadNotFound(t *testing.T) {
	f := newFS(t, nil)
	out := run(t, f, "read", `{"path":"missing.txt"}`)
	if !out.IsError || !strings.Contains(out.Text, "not found") {
		t.Fatalf("want not-found error, got IsError=%v text=%q", out.IsError, out.Text)
	}
}

func TestReadDirectoryIsError(t *testing.T) {
	f := newFS(t, map[string]string{"dir/x.txt": "x"})
	out := run(t, f, "read", `{"path":"dir"}`)
	if !out.IsError || !strings.Contains(out.Text, "directory") {
		t.Fatalf("want directory error, got %q", out.Text)
	}
}

func TestReadBinaryIsError(t *testing.T) {
	f := newFS(t, map[string]string{"bin": "abc\x00def"})
	out := run(t, f, "read", `{"path":"bin"}`)
	if !out.IsError || !strings.Contains(out.Text, "binary") {
		t.Fatalf("want binary error, got %q", out.Text)
	}
}

func TestReadTruncatesHugeFile(t *testing.T) {
	big := strings.Repeat("x", 1000)
	f := newFS(t, map[string]string{"big.txt": big}, WithMaxReadBytes(100))
	out := run(t, f, "read", `{"path":"big.txt"}`)

	if !strings.Contains(out.FileRead.Content, "[read truncated") {
		t.Fatalf("expected truncation marker, got %q", out.FileRead.Content)
	}
	// Content is capped near the byte budget (plus the marker text).
	if len(out.FileRead.Content) > 100+64 {
		t.Fatalf("content not truncated: %d bytes", len(out.FileRead.Content))
	}
}

func TestPathEscapeRejected(t *testing.T) {
	f := newFS(t, nil)
	out := run(t, f, "read", `{"path":"../../etc/passwd"}`)
	if !out.IsError || !strings.Contains(out.Text, "escapes") {
		t.Fatalf("want escape error, got %q", out.Text)
	}
}

func TestWriteNewFile(t *testing.T) {
	f := newFS(t, nil)
	out := run(t, f, "write", `{"path":"sub/new.txt","content":"hi"}`)
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	got, err := os.ReadFile(filepath.Join(f.root, "sub/new.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hi" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteRefusesUnreadOverwrite(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "original"})
	out := run(t, f, "write", `{"path":"a.txt","content":"clobbered"}`)
	if !out.IsError || !strings.Contains(out.Text, "read it first") {
		t.Fatalf("want refusal, got IsError=%v text=%q", out.IsError, out.Text)
	}
	// The original must be untouched.
	got, _ := os.ReadFile(filepath.Join(f.root, "a.txt"))
	if string(got) != "original" {
		t.Fatalf("file was modified: %q", got)
	}
}

func TestWriteOverwriteAfterRead(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "original"})
	run(t, f, "read", `{"path":"a.txt"}`)
	out := run(t, f, "write", `{"path":"a.txt","content":"updated"}`)
	if out.IsError {
		t.Fatalf("unexpected error after read: %s", out.Text)
	}
	got, _ := os.ReadFile(filepath.Join(f.root, "a.txt"))
	if string(got) != "updated" {
		t.Fatalf("content = %q", got)
	}
}

func TestEditUniqueMatch(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "foo bar baz"})
	out := run(t, f, "edit", `{"path":"a.txt","old_string":"bar","new_string":"qux"}`)
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	got, _ := os.ReadFile(filepath.Join(f.root, "a.txt"))
	if string(got) != "foo qux baz" {
		t.Fatalf("content = %q", got)
	}
}

func TestEditAmbiguousFailsLoudly(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "x x x"})
	out := run(t, f, "edit", `{"path":"a.txt","old_string":"x","new_string":"y"}`)
	if !out.IsError || !strings.Contains(out.Text, "ambiguous") {
		t.Fatalf("want ambiguous error, got IsError=%v text=%q", out.IsError, out.Text)
	}
	got, _ := os.ReadFile(filepath.Join(f.root, "a.txt"))
	if string(got) != "x x x" {
		t.Fatalf("file changed despite ambiguous edit: %q", got)
	}
}

func TestEditReplaceAll(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "x x x"})
	out := run(t, f, "edit", `{"path":"a.txt","old_string":"x","new_string":"y","replace_all":true}`)
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	got, _ := os.ReadFile(filepath.Join(f.root, "a.txt"))
	if string(got) != "y y y" {
		t.Fatalf("content = %q", got)
	}
}

func TestEditStringNotFound(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "abc"})
	out := run(t, f, "edit", `{"path":"a.txt","old_string":"zzz","new_string":"y"}`)
	if !out.IsError || !strings.Contains(out.Text, "not found") {
		t.Fatalf("want not-found error, got %q", out.Text)
	}
}

func TestGlobMatchesRecursively(t *testing.T) {
	f := newFS(t, map[string]string{
		"main.go":        "",
		"a/b/util.go":    "",
		"a/notes.txt":    "",
		"vendor/dep.go":  "",
		"a/b/c/deep.go":  "",
		"a/b/readme.md":  "",
		"a/b/c/d/x.json": "",
	})
	out := run(t, f, "glob", `{"pattern":"**/*.go"}`)
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	got := strings.Split(out.Text, "\n")
	want := []string{"a/b/c/deep.go", "a/b/util.go", "main.go", "vendor/dep.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("glob results = %v, want %v", got, want)
	}
}

func TestGlobWithBasePath(t *testing.T) {
	f := newFS(t, map[string]string{
		"cmd/smith/main.go": "",
		"cmd/tool/main.go":  "",
		"internal/x.go":     "",
	})
	out := run(t, f, "glob", `{"pattern":"*/main.go","path":"cmd"}`)
	got := strings.Split(out.Text, "\n")
	want := []string{"cmd/smith/main.go", "cmd/tool/main.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("glob = %v, want %v", got, want)
	}
}

func TestGlobNoMatches(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": ""})
	out := run(t, f, "glob", `{"pattern":"**/*.go"}`)
	if out.IsError || !strings.Contains(out.Text, "No files match") {
		t.Fatalf("want no-match message, got IsError=%v text=%q", out.IsError, out.Text)
	}
}

func TestGrepFindsLines(t *testing.T) {
	f := newFS(t, map[string]string{
		"a.go": "package main\nfunc Foo() {}\n",
		"b.go": "package main\nfunc Bar() {}\n",
		"c.md": "func Foo in docs\n",
	})
	out := run(t, f, "grep", `{"pattern":"func \\w+\\(","include":"*.go"}`)
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	lines := strings.Split(out.Text, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 matches, got %d: %q", len(lines), out.Text)
	}
	if !strings.Contains(out.Text, "a.go:2:func Foo() {}") || !strings.Contains(out.Text, "b.go:2:func Bar() {}") {
		t.Fatalf("unexpected matches: %q", out.Text)
	}
}

func TestGrepNoMatches(t *testing.T) {
	f := newFS(t, map[string]string{"a.go": "package main\n"})
	out := run(t, f, "grep", `{"pattern":"nonexistent"}`)
	if out.IsError || !strings.Contains(out.Text, "No matches") {
		t.Fatalf("want no-match message, got IsError=%v text=%q", out.IsError, out.Text)
	}
}

func TestGrepSkipsBinary(t *testing.T) {
	f := newFS(t, map[string]string{
		"a.go": "match here\n",
		"bin":  "match\x00here\n",
	})
	out := run(t, f, "grep", `{"pattern":"match"}`)
	if strings.Contains(out.Text, "bin:") {
		t.Fatalf("binary file should be skipped: %q", out.Text)
	}
	if !strings.Contains(out.Text, "a.go:1:") {
		t.Fatalf("text file should match: %q", out.Text)
	}
}

func TestGrepInvalidRegex(t *testing.T) {
	f := newFS(t, nil)
	out := run(t, f, "grep", `{"pattern":"("}`)
	if !out.IsError || !strings.Contains(out.Text, "invalid pattern") {
		t.Fatalf("want invalid-pattern error, got %q", out.Text)
	}
}

func TestGlobSkipsIgnoredDirs(t *testing.T) {
	f := newFS(t, map[string]string{
		"src/a.go":            "",
		".git/hooks/pre.go":   "",
		"node_modules/dep.go": "",
		".venv/lib/mod.go":    "",
	})
	out := run(t, f, "glob", `{"pattern":"**/*.go"}`)
	if out.Text != "src/a.go" {
		t.Fatalf("glob should skip .git/node_modules/.venv, got %q", out.Text)
	}
}

func TestGrepSkipsIgnoredDirs(t *testing.T) {
	f := newFS(t, map[string]string{
		"src/a.go":          "needle\n",
		".git/config":       "needle\n",
		"node_modules/x.js": "needle\n",
	})
	out := run(t, f, "grep", `{"pattern":"needle"}`)
	if !strings.Contains(out.Text, "src/a.go:1:") {
		t.Fatalf("grep should match source: %q", out.Text)
	}
	if strings.Contains(out.Text, ".git") || strings.Contains(out.Text, "node_modules") {
		t.Fatalf("grep should skip ignored dirs: %q", out.Text)
	}
}

func TestEditPreservesFileMode(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "foo bar"})
	abs := filepath.Join(f.root, "a.txt")
	if err := os.Chmod(abs, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	out := run(t, f, "edit", `{"path":"a.txt","old_string":"bar","new_string":"baz"}`)
	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Text)
	}
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after edit = %o, want 600", info.Mode().Perm())
	}
}

// --- Runtime integration: file_read emission and the permission gate. ---

func runtimeFor(t *testing.T, f *FS, perm tool.PermissionFunc) (*tool.Runtime, *eventlog.Log) {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tl := range f.Tools() {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("register %s: %v", tl.Def().Name, err)
		}
	}
	log := eventlog.New()
	return tool.NewRuntime(reg, log, tool.WithPermission(perm)), log
}

func readCall(name, args string) schema.Block {
	return schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindToolCall,
		Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{
			ToolUseID: "tu_1",
			Name:      name,
			Arguments: json.RawMessage(args),
		},
	}
}

func countKind(log *eventlog.Log, kind schema.Kind) int {
	n := 0
	for _, b := range log.Events() {
		if b.Kind == kind {
			n++
		}
	}
	return n
}

func TestRuntimeRecordsFileReadAndResult(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "hello"})
	rt, log := runtimeFor(t, f, tool.AllowAll)

	if _, err := rt.Execute(context.Background(), readCall("read", `{"path":"a.txt"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := countKind(log, schema.KindFileRead); got != 1 {
		t.Fatalf("want 1 file_read block, got %d", got)
	}
	if got := countKind(log, schema.KindToolResult); got != 1 {
		t.Fatalf("want 1 tool_result block, got %d", got)
	}

	var fr schema.Block
	for _, b := range log.Events() {
		if b.Kind == schema.KindFileRead {
			fr = b
		}
	}
	if fr.FileRead.ProducedBy != "tu_1" {
		t.Fatalf("file_read.ProducedBy = %q, want tu_1", fr.FileRead.ProducedBy)
	}
	if fr.FileRead.Source != "tool" {
		t.Fatalf("file_read.Source = %q, want tool", fr.FileRead.Source)
	}
	if fr.Provenance == nil || len(fr.Provenance.DerivedFrom) != 1 {
		t.Fatalf("file_read provenance not linked to the call: %+v", fr.Provenance)
	}
	if fr.Attribution == nil || fr.Attribution.Tool != "read" {
		t.Fatalf("file_read attribution = %+v", fr.Attribution)
	}
}

func TestReReadProducesNewFileReadBlock(t *testing.T) {
	f := newFS(t, map[string]string{"a.txt": "hello"})
	rt, log := runtimeFor(t, f, tool.AllowAll)

	for i := 0; i < 2; i++ {
		if _, err := rt.Execute(context.Background(), readCall("read", `{"path":"a.txt"}`)); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	if got := countKind(log, schema.KindFileRead); got != 2 {
		t.Fatalf("re-reads must stay visible: want 2 file_read blocks, got %d", got)
	}
}

func TestPermissionDeniedBlocksWrite(t *testing.T) {
	f := newFS(t, nil)
	deny := func(context.Context, tool.Call) tool.Decision { return tool.Denied("nope") }
	rt, log := runtimeFor(t, f, deny)

	res, err := rt.Execute(context.Background(), readCall("write", `{"path":"new.txt","content":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.ToolResult == nil || !res.ToolResult.IsError {
		t.Fatalf("want an error tool_result on denial, got %+v", res.ToolResult)
	}
	if _, statErr := os.Stat(filepath.Join(f.root, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatal("denied write must not create the file")
	}
	if got := countKind(log, schema.KindFileRead); got != 0 {
		t.Fatalf("a denied call must not record a file_read block, got %d", got)
	}
}
