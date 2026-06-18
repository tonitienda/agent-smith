// Package skill discovers portable skills — instruction bundles the model loads
// on demand — from user- and project-level directories and exposes them so a
// face can offer them to the model (AS-034, PRD §7.7, §4). A skill is a
// directory holding a `SKILL.md` file: a `---`-fenced frontmatter carrying at
// least `name` and `description`, then a Markdown body of instructions. The
// format mirrors Claude Code's so a skill authored for it loads unmodified (the
// portability thesis, §4); extra frontmatter keys (e.g. `expected_outcome`,
// `completion`) are preserved in Meta for AS-047 to interpret, not used here.
//
// Discovery covers two locations, lowest precedence first:
//
//   - user level — <UserConfigDir>/smith/skills/<name>/SKILL.md (matches the
//     layered-config, memory-file, and custom-command convention; applies to
//     every project);
//   - project level — .agent-smith/skills/<name>/SKILL.md under the working tree.
//
// Project wins on a name collision (the shadowed user skill is dropped). The
// skill name is its frontmatter `name`, falling back to the directory base name.
//
// The package is face- and runtime-agnostic and depends only on the stdlib: it
// returns plain Skill descriptors plus skill-load events for the log. The wiring
// (cmd/smith) adapts the skills into a single "skill" tool the model invokes by
// name and seeds the load events, so this package never imports the tool runtime
// or any face.
package skill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileName is the manifest a skill directory must contain to be discovered.
const fileName = "SKILL.md"

// Skill is one discovered portable skill: its instruction body plus the metadata
// a face needs to expose, attribute, and (later, AS-047) analyze it.
type Skill struct {
	// Name is the invocation token — the frontmatter `name`, falling back to the
	// skill directory's base name.
	Name string
	// Description is the one-line summary from frontmatter shown to the model so it
	// can decide when to invoke the skill (may be empty).
	Description string
	// Source is the absolute path of the SKILL.md file, so a face can attribute the
	// skill to where it came from.
	Source string
	// Scope is "project" or "user" — where the skill was found.
	Scope string
	// Body is the Markdown instructions that enter the context when the skill is
	// invoked.
	Body string
	// Meta carries frontmatter keys beyond name/description (e.g. expected_outcome,
	// completion), preserved verbatim for AS-047 to interpret. Never nil.
	Meta map[string]string
}

// UserDir returns the user-level skills directory — <UserConfigDir>/smith/skills,
// matching the memory-file and custom-command convention. It returns "" when the
// OS reports no user config dir, in which case the user level is skipped.
func UserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "smith", "skills")
}

// ProjectDir returns the project-level skills directory for the working tree
// rooted at wd.
func ProjectDir(wd string) string {
	return filepath.Join(wd, ".agent-smith", "skills")
}

// Load discovers the skills visible from the given directories, project winning
// over user on a name collision. Either directory may be "" (skipped) or absent
// (no error — a project simply has no skills). Directories without a valid
// SKILL.md, or whose name can't be invoked, are skipped. The result is sorted by
// name.
func Load(userDir, projectDir string) ([]Skill, error) {
	user, err := loadDir(userDir, "user")
	if err != nil {
		return nil, err
	}
	project, err := loadDir(projectDir, "project")
	if err != nil {
		return nil, err
	}

	byName := make(map[string]Skill, len(user)+len(project))
	for _, s := range user {
		byName[s.Name] = s
	}
	for _, s := range project {
		byName[s.Name] = s // project shadows user
	}

	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadDir reads every immediate subdirectory of dir that holds a SKILL.md into a
// Skill. A missing dir yields no skills and no error (the locations are optional);
// a read error on an existing manifest is surfaced so a broken setup fails loudly
// rather than silently dropping a skill.
func loadDir(dir, scope string) ([]Skill, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		// Resolve symlinks (os.Stat, not e.IsDir) so a skill directory linked in by
		// a dotfile manager is discovered rather than silently skipped.
		if fi, err := os.Stat(filepath.Join(dir, e.Name())); err != nil || !fi.IsDir() {
			continue
		}
		manifest := filepath.Join(dir, e.Name(), fileName)
		raw, err := os.ReadFile(manifest)
		if err != nil {
			if os.IsNotExist(err) {
				continue // a directory without a SKILL.md is not a skill
			}
			return nil, err
		}
		name, desc, meta, body := parseFrontmatter(string(raw))
		if name == "" {
			name = e.Name() // fall back to the directory name
		}
		// A name that can't be referenced (carrying whitespace or a path
		// separator — slash or, on Windows, backslash) is skipped rather than
		// registered un-invocably.
		if name == "" || strings.ContainsAny(name, " \t\n/\\") {
			continue
		}
		abs, err := filepath.Abs(manifest)
		if err != nil {
			abs = manifest
		}
		skills = append(skills, Skill{
			Name:        name,
			Description: desc,
			Source:      abs,
			Scope:       scope,
			Body:        body,
			Meta:        meta,
		})
	}
	return skills, nil
}

// parseFrontmatter splits an optional `---`-fenced frontmatter from the body and
// reads name, description, and every other key (preserved in meta). It works on
// whole lines — the opening fence is a `---` first line and the closing fence the
// next `---` line — so it handles an empty frontmatter (`---`/`---`) and a file
// that ends exactly at the closing fence with no trailing newline. Content
// without an opening fence (or with an unterminated one) is treated entirely as
// the body, so a bare Markdown skill still loads with the directory name.
func parseFrontmatter(content string) (name, desc string, meta map[string]string, body string) {
	meta = map[string]string{}
	// Normalize Windows CRLF so fence detection and line splitting work regardless
	// of how the file was checked out or saved.
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return "", "", meta, content
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return "", "", meta, content // unterminated fence: treat as plain body
	}
	for _, line := range lines[1:closeIdx] {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		case "":
			// skip blank/garbage lines
		default:
			meta[key] = val
		}
	}
	body = strings.TrimLeft(strings.Join(lines[closeIdx+1:], "\n"), "\n")
	return name, desc, meta, body
}
