package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/tool"
)

// maxGrepMatches caps the lines grep returns so a broad pattern cannot flood the
// window; the Runtime truncates further if needed. The cap is reported when hit.
const maxGrepMatches = 500

// globTool lists files whose path matches a shell-style pattern, supporting **
// to span directories. Results are project-relative and sorted for stable,
// cache-friendly output.
type globTool struct{ fs *FS }

func (t *globTool) Def() tool.Def {
	return tool.Def{
		Name: "glob",
		Description: "List files matching a glob pattern (supports * ? [..] and ** for any depth), " +
			"relative to the project root. Results are sorted alphabetically.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["pattern"],
  "properties": {
    "pattern": {"type": "string", "description": "Glob pattern, e.g. **/*.go or cmd/*/main.go."},
    "path": {"type": "string", "description": "Optional subdirectory to search under, relative to the project root."}
  }
}`),
	}
}

func (t *globTool) Run(_ context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return errResult("pattern is required"), nil
	}
	if _, err := path.Match(in.Pattern, ""); err != nil {
		return errResult("invalid pattern %q: %v", in.Pattern, err), nil
	}

	base, rel, err := t.fs.searchBase(in.Path)
	if err != nil {
		return errResult("%v", err), nil
	}

	var matches []string
	walkErr := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return skipUnreadableDir(d, err)
		}
		name := t.fs.rel(p)
		candidate := name
		if rel != "" {
			candidate = strings.TrimPrefix(strings.TrimPrefix(name, rel), "/")
		}
		ok, mErr := matchPath(in.Pattern, candidate)
		if mErr != nil {
			return mErr
		}
		if ok {
			matches = append(matches, name)
		}
		return nil
	})
	if walkErr != nil {
		return errResult("glob failed: %v", walkErr), nil
	}

	if len(matches) == 0 {
		return tool.Output{Text: "No files match " + in.Pattern}, nil
	}
	sort.Strings(matches)
	return tool.Output{Text: strings.Join(matches, "\n")}, nil
}

// grepTool searches file contents for a regular expression, returning matching
// lines as path:line:text. Pure Go (regexp + a filesystem walk), no ripgrep
// dependency. Binary files are skipped.
type grepTool struct{ fs *FS }

func (t *grepTool) Def() tool.Def {
	return tool.Def{
		Name: "grep",
		Description: "Search file contents for a regular expression (Go regexp syntax), returning " +
			"matching lines as path:line:text. Optionally restrict to a subdirectory and to filenames " +
			"matching an include glob. Binary files are skipped.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["pattern"],
  "properties": {
    "pattern": {"type": "string", "description": "Regular expression (Go regexp syntax)."},
    "path": {"type": "string", "description": "Optional subdirectory to search under, relative to the project root."},
    "include": {"type": "string", "description": "Optional glob limiting which filenames are searched, e.g. *.go."}
  }
}`),
	}
}

func (t *grepTool) Run(_ context.Context, args json.RawMessage) (tool.Output, error) {
	var in struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err), nil
	}
	if in.Pattern == "" {
		return errResult("pattern is required"), nil
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return errResult("invalid pattern: %v", err), nil
	}
	if in.Include != "" {
		if _, err := path.Match(in.Include, ""); err != nil {
			return errResult("invalid include glob %q: %v", in.Include, err), nil
		}
	}

	base, _, err := t.fs.searchBase(in.Path)
	if err != nil {
		return errResult("%v", err), nil
	}

	var matches []string
	capped := false
	walkErr := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return skipUnreadableDir(d, err)
		}
		if in.Include != "" {
			ok, mErr := path.Match(in.Include, filepath.Base(p))
			if mErr != nil {
				return mErr
			}
			if !ok {
				return nil
			}
		}
		hits, err := grepFile(p, t.fs.rel(p), re, maxGrepMatches-len(matches))
		if err != nil {
			return nil // unreadable file: skip, don't fail the whole search
		}
		matches = append(matches, hits...)
		if len(matches) >= maxGrepMatches {
			capped = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return errResult("grep failed: %v", walkErr), nil
	}

	if len(matches) == 0 {
		return tool.Output{Text: "No matches for " + in.Pattern}, nil
	}
	out := strings.Join(matches, "\n")
	if capped {
		out += fmt.Sprintf("\n\n[results capped at %d matches]", maxGrepMatches)
	}
	return tool.Output{Text: out}, nil
}

// grepFile returns up to limit "path:line:text" matches of re in the file at
// abs, labeled with rel. Binary files (those containing a NUL byte in the first
// scanned chunk) are skipped by returning no matches.
func grepFile(abs, rel string, re *regexp.Regexp, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var matches []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		b := scanner.Bytes()
		if line == 1 && bytes.IndexByte(b, 0) >= 0 {
			return nil, nil // binary file
		}
		if re.Match(b) {
			matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, line, string(b)))
			if len(matches) >= limit {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

// searchBase resolves an optional subdirectory under the root for a search tool.
// It returns the absolute base directory to walk and its project-relative path
// (empty for the root). A path that is not a directory is an error.
func (f *FS) searchBase(sub string) (abs, rel string, err error) {
	if strings.TrimSpace(sub) == "" {
		return f.root, "", nil
	}
	abs, err = f.resolve(sub)
	if err != nil {
		return "", "", err
	}
	info, statErr := os.Stat(abs)
	if statErr != nil {
		return "", "", fmt.Errorf("path %q not found", sub)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("path %q is not a directory", sub)
	}
	return abs, f.rel(abs), nil
}

// skipUnreadableDir turns a walk error into a skip of the offending directory
// (so one unreadable subtree does not abort the whole walk) and propagates a
// non-directory error unchanged.
func skipUnreadableDir(d fs.DirEntry, err error) error {
	if err == nil {
		return nil
	}
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// matchPath reports whether the slash-separated path name matches pattern.
// Beyond the per-segment wildcards path.Match understands (* ? [..]), the
// segment ** matches any number of path segments, including zero — so **/*.go
// matches main.go and a/b/main.go alike.
func matchPath(pattern, name string) (bool, error) {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pat, name []string) (bool, error) {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true, nil // trailing ** matches any remainder
			}
			for i := 0; i <= len(name); i++ {
				ok, err := matchSegments(rest, name[i:])
				if err != nil || ok {
					return ok, err
				}
			}
			return false, nil
		}
		if len(name) == 0 {
			return false, nil
		}
		ok, err := path.Match(pat[0], name[0])
		if err != nil || !ok {
			return false, err
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0, nil
}
