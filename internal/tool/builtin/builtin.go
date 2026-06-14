// Package builtin holds the harness's first-party tools — the minimum set a
// credible coding agent needs (AS-014, PRD §7.2): file read/write/edit, glob,
// and regex grep. They implement the tool.Tool interface and plug into the
// tool.Runtime like any other tool, so they inherit its argument validation,
// permission gate (AS-016), output truncation, and event-log provenance for
// free and never touch the log or the wire format themselves.
//
// All five tools share one FS, which fixes the session working directory they
// resolve paths against and rejects any path that escapes that root (a lexical
// check — symlink traversal is a documented V1 limit, see docs/SECURITY.md once
// AS-016 lands). FS also carries the read-tracking set the write tool consults
// to honor the "never overwrite a file this session has not read" safety
// convention.
//
// grep is implemented in pure Go (regexp + a filesystem walk), not by shelling
// out to ripgrep, so the binary stays self-contained and dependency-free
// (stdlib-only, per the repo conventions).
package builtin

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tonitienda/agent-smith/internal/tool"
)

// FS is the shared configuration and state for the file and search tools: the
// root directory paths resolve against, a cap on how much of a file the read
// tool returns, and the set of files read this session (so write can refuse to
// clobber an unread file). It is safe for concurrent use — the Runtime may run
// several tool calls against it at once (AS-019).
type FS struct {
	root         string
	maxReadBytes int
	reads        *readSet
}

// ignoredDirs are directory names the glob and grep walks skip by default:
// heavy VCS/dependency trees that are essentially never the search target and
// would otherwise dominate a walk in a real project (a .git or node_modules can
// hold orders of magnitude more files than the source itself). This is a fixed
// default for V1; making the ignore set configurable and gitignore-aware is a
// planned follow-up. The root directory of a search is never skipped even if it
// shares one of these names, so an explicit search inside one still works.
var ignoredDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".venv":        true,
}

// DefaultMaxReadBytes caps the bytes the read tool returns for a single read
// before it truncates with an explicit marker, so a huge file cannot flood the
// context window. The Runtime applies a second, independent cap to the recorded
// block (tool.DefaultMaxResultBytes); this one keeps the tool's own output sane.
const DefaultMaxReadBytes = 64 * 1024

// Option configures an FS.
type Option func(*FS)

// WithMaxReadBytes overrides the read tool's per-read byte cap. A non-positive n
// is ignored.
func WithMaxReadBytes(n int) Option {
	return func(f *FS) {
		if n > 0 {
			f.maxReadBytes = n
		}
	}
}

// NewFS builds an FS rooted at root, which is resolved to an absolute path so
// every later resolution is stable regardless of the process working directory.
func NewFS(root string, opts ...Option) (*FS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("builtin: resolve root %q: %w", root, err)
	}
	f := &FS{
		root:         filepath.Clean(abs),
		maxReadBytes: DefaultMaxReadBytes,
		reads:        newReadSet(),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f, nil
}

// Tools returns the file and search tools backed by this FS, ready to register
// with a tool.Registry.
func (f *FS) Tools() []tool.Tool {
	return []tool.Tool{
		&readTool{fs: f},
		&writeTool{fs: f},
		&editTool{fs: f},
		&globTool{fs: f},
		&grepTool{fs: f},
	}
}

// resolve turns a model-supplied path into an absolute path inside the root,
// rejecting any path that would escape it. The check is lexical: the cleaned
// absolute path must be the root itself or live beneath it. Symlinks that point
// outside the root are not followed-and-checked in V1 (a documented limit).
func (f *FS) resolve(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(f.root, abs)
	}
	abs = filepath.Clean(abs)

	rel, err := filepath.Rel(f.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the project root", p)
	}
	return abs, nil
}

// rel renders an absolute path inside the root as a clean, slash-separated path
// relative to the root, for stable model-facing output. Paths outside the root
// (which resolve already rejects) fall back to the input.
func (f *FS) rel(abs string) string {
	r, err := filepath.Rel(f.root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(r)
}

// readSet records which files have been read this session, so the write tool can
// refuse to overwrite a file the agent has not seen. Keys are absolute paths.
type readSet struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newReadSet() *readSet { return &readSet{seen: make(map[string]bool)} }

func (s *readSet) mark(abs string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[abs] = true
}

func (s *readSet) has(abs string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[abs]
}
