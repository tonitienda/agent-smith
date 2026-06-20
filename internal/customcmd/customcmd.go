// Package customcmd discovers user- and project-defined slash commands from
// Markdown files and turns them into prompt templates the model runs (AS-033,
// PRD §7.6, §4). It completes the slash-command story the AS-022 framework
// started: a built-in command is Go code, a custom command is a `.md` file whose
// body is a prompt template the face submits as a user turn.
//
// Discovery covers two locations, lowest precedence first:
//
//   - user level — <UserConfigDir>/smith/commands/*.md (matches the layered-config
//     and memory-file convention, applies to every project);
//   - project level — .agent-smith/commands/*.md under the working tree.
//
// Project wins on a name collision (the shadowed user command is dropped and the
// winner is marked as overriding it, so /help can say so). The command name is
// the file's base name without the .md extension.
//
// File format mirrors Claude Code's so a project set up for it works unmodified
// (the portability thesis, §4): an optional `---`-fenced frontmatter carrying
// `description` and `argument-hint`, then a Markdown body used as the prompt
// template. Two substitutions are honored in the body: $ARGUMENTS expands to the
// whole argument string and $1..$n to positional arguments (a missing positional
// expands to empty).
//
// The package is face-agnostic and depends only on the stdlib: it returns plain
// Command descriptors with an Expand method. The wiring (cmd/smith) adapts each
// into a command.Command whose handler returns the expanded template as the
// prompt to run, so this package never imports the command framework or any face.
package customcmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Command is one discovered custom command: a prompt template plus the metadata a
// face needs to list and attribute it.
type Command struct {
	// Name is the invocation token without the leading slash — the file's base
	// name without the .md extension.
	Name string
	// Description is the one-line summary from frontmatter (may be empty).
	Description string
	// ArgHint is the human-readable argument spec from frontmatter's
	// argument-hint (may be empty), shown in help like a built-in's Args.
	ArgHint string
	// Source is the absolute path of the defining file, so a face can attribute
	// the command to where it came from.
	Source string
	// Scope is "project" or "user" — where the file was found.
	Scope string
	// Overrides is true when this command shadows a user-level command of the same
	// name (project beat user), so /help can note the override.
	Overrides bool

	body string
}

// Expand renders the command's template against args: $ARGUMENTS becomes the
// args joined by a space and $1..$n the positional arguments, with a missing
// positional expanding to empty. It is pure and depends only on body and args.
func (c Command) Expand(args []string) string {
	return expand(c.body, args)
}

var placeholder = regexp.MustCompile(`\$(ARGUMENTS|[0-9]+)`)

func expand(body string, args []string) string {
	return placeholder.ReplaceAllStringFunc(body, func(m string) string {
		tok := m[1:]
		if tok == "ARGUMENTS" {
			return strings.Join(args, " ")
		}
		n, _ := strconv.Atoi(tok) // 1-based; regexp guarantees digits
		if n >= 1 && n <= len(args) {
			return args[n-1]
		}
		return ""
	})
}

// UserDir returns the user-level commands directory — <UserConfigDir>/smith/
// commands, matching the memory-file and config convention. It returns "" when
// the OS reports no user config dir, in which case the user level is skipped.
func UserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "smith", "commands")
}

// ProjectDir returns the project-level commands directory for the working tree
// rooted at wd.
func ProjectDir(wd string) string {
	return filepath.Join(wd, ".agent-smith", "commands")
}

// Load discovers the custom commands visible from the given directories, project
// winning over user on a name collision. Either directory may be "" (skipped) or
// absent (no error — a project simply has no custom commands). Files that don't
// parse into a valid, invocable name are skipped. The result is sorted by name.
func Load(userDir, projectDir string) ([]Command, error) {
	user, err := loadDir(userDir, "user")
	if err != nil {
		return nil, err
	}
	project, err := loadDir(projectDir, "project")
	if err != nil {
		return nil, err
	}

	byName := make(map[string]Command, len(user)+len(project))
	for _, c := range user {
		byName[c.Name] = c
	}
	for _, c := range project {
		if _, shadowed := byName[c.Name]; shadowed {
			c.Overrides = true
		}
		byName[c.Name] = c
	}

	out := make([]Command, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadDir reads every *.md in dir into a Command. A missing dir yields no
// commands and no error (the locations are optional); a read error on an existing
// file is surfaced so a broken setup fails loudly rather than dropping a command.
func loadDir(dir, scope string) ([]Command, error) {
	if dir == "" {
		return nil, nil
	}
	return loadFS(os.DirFS(dir), dir, scope)
}

// loadFS scans fsys — the filesystem rooted at base — for command files, reading
// each through io/fs so the discovery logic is decoupled from the OS and testable
// with fstest.MapFS. base is used only to attribute each command's Source to an
// absolute on-disk path; pass "" when scanning an in-memory tree, in which case
// Source is the file name within fsys. os.DirFS bounds reads to base, so a file
// cannot be read from outside the scanned root.
func loadFS(fsys fs.FS, base, scope string) ([]Command, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cmds []Command
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		// A name that can't be typed as a slash command (empty, or carrying
		// whitespace/slash) is skipped rather than registered un-invocably.
		if name == "" || strings.ContainsAny(name, " \t\n/") {
			continue
		}
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // raced away between ReadDir and read
			}
			return nil, err
		}
		desc, hint, body := parseFrontmatter(string(raw))
		cmds = append(cmds, Command{
			Name:        name,
			Description: desc,
			ArgHint:     hint,
			Source:      source(base, e.Name()),
			Scope:       scope,
			body:        body,
		})
	}
	return cmds, nil
}

// source builds a discovered file's display path: the absolute on-disk path of
// base/name when base is set, or name alone within an in-memory tree when it
// is "".
func source(base, name string) string {
	if base == "" {
		return name
	}
	p := filepath.Join(base, name)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// parseFrontmatter splits an optional `---`-fenced frontmatter from the body and
// reads the description and argument-hint keys from it. Content without a fence
// (or with an unterminated one) is treated entirely as the body, so a plain
// Markdown prompt file works with no ceremony.
func parseFrontmatter(content string) (desc, hint, body string) {
	// Normalize Windows CRLF so the fence detection and line splitting below work
	// regardless of how the file was checked out or saved.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", "", content
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return "", "", content
	}
	front := content[4 : 4+end]
	body = strings.TrimLeft(content[4+end+5:], "\n")
	for _, line := range strings.Split(front, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.TrimSpace(key) {
		case "description":
			desc = val
		case "argument-hint":
			hint = val
		}
	}
	return desc, hint, body
}
