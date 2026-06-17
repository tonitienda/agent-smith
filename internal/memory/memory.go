// Package memory loads the project's memory files — AGENTS.md, AGENT.md and
// CLAUDE.md — and merges them hierarchically into model-facing context (AS-032,
// PRD §7.3). It is the portability wedge applied to instructions: a project set
// up for Claude Code (CLAUDE.md) or Codex/AGENTS (AGENT.md/AGENTS.md) works in
// Agent Smith unmodified, because all three filenames are honored as equivalent
// at every level of the hierarchy.
//
// A memory file becomes a system text block on the event log — the same shape
// /goal uses (AS-040): identified by Provenance.Producer and carrying its source
// path so /context (AS-026) can attribute it. The log is the single source of
// truth (PRD D3), so a memory block is projected, priced, and /clean-able like
// any other segment; nothing in the loop or projection needs to special-case it.
//
// Discovery walks user → project → directory, lowest precedence first: the
// user-level dir, then every ancestor directory of the working path from the
// outermost down to the working directory, so a deeper (more specific) file
// loads last. @import-style includes are out of scope here — see AS-082.
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonitienda/agent-smith/schema"
)

// Producer identifies the memory blocks this package appends to the log, so they
// are recognizable on the event stream without spending a frozen content kind on
// them (mirrors goal.Producer).
const Producer = "memory"

// extPath is the block Ext key carrying a memory block's source file path. Ext
// is the schema's forward-compat escape hatch (schema.Block.Ext), so attaching
// the origin path here needs no schema change.
const extPath = "memory_path"

// Filenames are the memory-file conventions honored at each directory level, in
// the deterministic order applied when several coexist in one directory. All
// three are treated as equivalent — the portability thesis — so a project with
// only CLAUDE.md behaves identically to one with only AGENT.md.
var Filenames = []string{"AGENTS.md", "AGENT.md", "CLAUDE.md"}

// UserDir returns the user-level memory directory — the per-user smith config
// dir, matching the layered-config convention (os.UserConfigDir()/smith). A
// memory file placed here applies to every project. It returns "" when the OS
// reports no user config dir, in which case the user level is simply skipped.
func UserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "smith")
}

// Discover returns the paths of the memory files visible from wd, lowest
// precedence first: the user-level dir (skipped when userDir is ""), then each
// ancestor directory from the outermost down to wd. Within a directory the
// Filenames order is honored, and each file appears at most once.
func Discover(userDir, wd string) []string {
	var dirs []string
	if userDir != "" {
		dirs = append(dirs, userDir)
	}
	dirs = append(dirs, ancestors(wd)...)

	var paths []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		for _, name := range Filenames {
			p := filepath.Join(dir, name)
			if seen[p] {
				continue
			}
			// Skip a clean absence or a directory by that name; include anything
			// else — including a stat that failed for a reason other than
			// non-existence (e.g. a permission error) — so Load reads it and fails
			// loudly rather than silently dropping guidance the user expects.
			fi, err := os.Stat(p)
			switch {
			case err == nil && fi.IsDir():
				continue
			case os.IsNotExist(err):
				continue
			default:
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// Load discovers and reads the memory files visible from wd into model-facing
// memory blocks, in precedence order (lowest first), ready to append to a fresh
// session's log. Empty or whitespace-only files are skipped so they never add an
// empty segment to /context. A read error other than a file racing away is
// returned, so a malformed setup fails loudly rather than silently dropping
// guidance the user expects the model to follow.
func Load(userDir, wd string) ([]schema.Block, error) {
	paths := Discover(userDir, wd)
	var blocks []schema.Block
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue // raced away between Discover and read
			}
			return nil, fmt.Errorf("read memory file %s: %w", p, err)
		}
		content := string(data)
		if strings.TrimSpace(content) == "" {
			continue
		}
		blocks = append(blocks, Block(p, content))
	}
	return blocks, nil
}

// Block builds a model-facing memory block carrying content, attributed to its
// source path so /context can show where it came from. It mirrors the /goal
// pattern: a system text block on the log, identified by Producer.
func Block(path, content string) schema.Block {
	b := schema.Block{
		ID:          schema.NewID(),
		Kind:        schema.KindText,
		Role:        schema.RoleSystem,
		Text:        &schema.TextBody{Text: content},
		Provenance:  &schema.Provenance{Producer: Producer},
		Attribution: &schema.Attribution{Hook: Producer},
	}
	if raw, err := json.Marshal(path); err == nil {
		b.Ext = map[string]json.RawMessage{extPath: raw}
	}
	return b
}

// Source returns a memory block's source file path. ok is false for any block
// this package did not produce, so a caller (e.g. the /context Origin column)
// can fall back to treating it like any other segment.
func Source(b schema.Block) (path string, ok bool) {
	if b.Provenance == nil || b.Provenance.Producer != Producer {
		return "", false
	}
	raw, present := b.Ext[extPath]
	if !present {
		return "", false
	}
	if err := json.Unmarshal(raw, &path); err != nil {
		return "", false
	}
	return path, true
}

// ancestors returns wd and each of its parent directories, outermost first, so
// the working directory (the most specific level) sorts last and therefore loads
// last — highest precedence. Paths are absolute and cleaned.
func ancestors(wd string) []string {
	abs, err := filepath.Abs(wd)
	if err != nil {
		abs = filepath.Clean(wd)
	}
	var chain []string
	for {
		chain = append(chain, abs)
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}
