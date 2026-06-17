// Package config is the layered configuration substrate (AS-031): built-in
// defaults, a user file, a project file, and env/flag overrides merged into one
// effective view, with the winning source recorded for every leaf.
//
// It is the substrate the fast-follow features hang off — provider/model
// defaults, permission rules (AS-016), pricing overrides (AS-020), sub-agent
// toggles (PRD Appendix C.3), personality (Appendix D), MCP servers, hooks.
// Rather than each feature parsing its own file ad hoc, they read typed values
// out of one merged Config.
//
// The on-disk format is JSON (stdlib only — see adr-0002). The structure is
// open and nested: unknown keys are preserved and merely warned about, never
// fatal, mirroring the schema's additive-only, tolerate-unknown ethos (PRD D2).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// Source identifies which layer supplied a value.
type Source struct {
	// Layer is the precedence band: "default", "user", "project", "env", or
	// "flag".
	Layer string
	// Origin is the concrete provenance — a file path for file layers, or a
	// short label like "env"/"flags"/"built-in" — so a warning can name where a
	// key actually came from.
	Origin string
}

func (s Source) String() string {
	if s.Origin == "" || s.Origin == s.Layer {
		return s.Layer
	}
	return fmt.Sprintf("%s (%s)", s.Layer, s.Origin)
}

// Layer is one band of configuration. Values is the decoded JSON object
// (nested map[string]any); a nil Values contributes nothing, so an absent file
// is just an empty layer.
type Layer struct {
	Source Source
	Values map[string]any
}

// FileLayer reads a JSON config file into a Layer tagged with the given
// precedence band. A missing file is not an error — it yields an empty layer so
// an absent user or project file simply contributes nothing. Origin is set to
// path either way, so warnings can name the file.
func FileLayer(layer, path string) (Layer, error) {
	l := Layer{Source: Source{Layer: layer, Origin: path}}
	data, err := os.ReadFile(path) //nolint:gosec // path is a known config location, not user input
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return l, nil
	}
	if err := json.Unmarshal(data, &l.Values); err != nil {
		return l, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return l, nil
}

// MapLayer wraps an in-memory map (env or flag overrides) as a Layer.
func MapLayer(layer, origin string, values map[string]any) Layer {
	return Layer{Source: Source{Layer: layer, Origin: origin}, Values: values}
}

// Config is the merged, effective configuration. Layers are merged
// lowest-to-highest precedence: a scalar or list at a path is replaced by the
// highest layer that sets it, while nested objects deep-merge key by key. Every
// resulting leaf remembers the Source that won it.
type Config struct {
	tree    map[string]any    // effective nested view
	sources map[string]Source // dotted leaf path -> winning source
}

// New merges layers (lowest precedence first) into an effective Config.
func New(layers ...Layer) *Config {
	c := &Config{tree: map[string]any{}, sources: map[string]Source{}}
	for _, l := range layers {
		if l.Values != nil {
			mergeMap(c.tree, l.Values, "", l.Source, c.sources)
		}
	}
	return c
}

// mergeMap deep-merges src into dst. Objects recurse; scalars and lists replace
// (a higher layer's list wins wholesale — list semantics are override, not
// append, so precedence stays predictable). Replacing an object with a leaf, or
// vice versa, clears the stale source entries underneath the old value.
func mergeMap(dst, src map[string]any, prefix string, source Source, sources map[string]Source) {
	for k, v := range src {
		path := joinPath(prefix, k)
		if sub, ok := v.(map[string]any); ok {
			child, ok := dst[k].(map[string]any)
			if !ok {
				child = map[string]any{}
				dst[k] = child
				delete(sources, path) // a leaf is becoming an object
			}
			mergeMap(child, sub, path, source, sources)
			continue
		}
		if _, wasMap := dst[k].(map[string]any); wasMap {
			clearPrefix(sources, path) // an object is becoming a leaf
		}
		dst[k] = v
		sources[path] = source
	}
}

// clearPrefix removes the source entry at path and every entry beneath it.
func clearPrefix(sources map[string]Source, path string) {
	prefix := path + "."
	for p := range sources {
		if p == path || strings.HasPrefix(p, prefix) {
			delete(sources, p)
		}
	}
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// lookup resolves a dotted path to its leaf value and source. ok is false when
// no layer sets the path (or it names an interior object rather than a leaf).
func (c *Config) lookup(path string) (any, Source, bool) {
	src, ok := c.sources[path]
	if !ok {
		return nil, Source{}, false
	}
	cur := any(c.tree)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, Source{}, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, Source{}, false
		}
	}
	return cur, src, true
}

// String returns the string at path. ok is false if unset or not a string.
func (c *Config) String(path string) (value string, source Source, ok bool) {
	v, s, ok := c.lookup(path)
	if !ok {
		return "", s, false
	}
	str, ok := v.(string)
	return str, s, ok
}

// Bool returns the bool at path. ok is false if unset or not a bool.
func (c *Config) Bool(path string) (value bool, source Source, ok bool) {
	v, s, ok := c.lookup(path)
	if !ok {
		return false, s, false
	}
	b, ok := v.(bool)
	return b, s, ok
}

// Float64 returns the number at path. ok is false if unset or not a number.
// JSON decodes every number to float64; native int/int64 (from an in-memory
// MapLayer, e.g. env/flag overrides) are accepted too.
func (c *Config) Float64(path string) (value float64, source Source, ok bool) {
	v, s, ok := c.lookup(path)
	if !ok {
		return 0, s, false
	}
	switch n := v.(type) {
	case float64:
		return n, s, true
	case float32:
		return float64(n), s, true
	case int:
		return float64(n), s, true
	case int64:
		return float64(n), s, true
	case int32:
		return float64(n), s, true
	default:
		return 0, s, false
	}
}

// Int returns the integer at path. ok is false if unset, not a number, or not a
// whole value.
func (c *Config) Int(path string) (value int, source Source, ok bool) {
	f, s, ok := c.Float64(path)
	if !ok || f != float64(int(f)) {
		return 0, s, false
	}
	return int(f), s, true
}

// Strings returns the string list at path. ok is false if unset, not a list, or
// any element is not a string. JSON decodes a list to []any; a native []string
// (from an in-memory MapLayer) is accepted too.
func (c *Config) Strings(path string) (value []string, source Source, ok bool) {
	v, s, ok := c.lookup(path)
	if !ok {
		return nil, s, false
	}
	switch list := v.(type) {
	case []string:
		return append([]string(nil), list...), s, true
	case []any:
		out := make([]string, 0, len(list))
		for _, e := range list {
			str, ok := e.(string)
			if !ok {
				return nil, s, false
			}
			out = append(out, str)
		}
		return out, s, true
	default:
		return nil, s, false
	}
}

// Entry is one resolved leaf in the effective config.
type Entry struct {
	Path   string
	Value  any
	Source Source
}

// Effective returns every resolved leaf, sorted by path — the data behind
// `smith config`. Each entry carries the value and the layer that won it.
func (c *Config) Effective() []Entry {
	out := make([]Entry, 0, len(c.sources))
	for path, src := range c.sources {
		v, _, _ := c.lookup(path)
		out = append(out, Entry{Path: path, Value: v, Source: src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Warning flags a config key whose top-level section is not recognized. The key
// is preserved (forward-compat, PRD D2); the warning just tells the operator
// where an unknown key lives so a typo does not pass silently.
type Warning struct {
	Path   string
	Source Source
}

func (w Warning) String() string {
	return fmt.Sprintf("unknown config key %q in %s", w.Path, w.Source.Origin)
}

// Unknown returns a warning for every leaf whose first path segment is not in
// known, sorted by path. Recognized sections register their top-level key (e.g.
// "model", "permissions", "subagents") so anything else surfaces as a warning
// without being dropped.
func (c *Config) Unknown(known ...string) []Warning {
	set := make(map[string]bool, len(known))
	for _, k := range known {
		set[k] = true
	}
	var out []Warning
	for path, src := range c.sources {
		top := path
		if i := strings.IndexByte(path, '.'); i >= 0 {
			top = path[:i]
		}
		if !set[top] {
			out = append(out, Warning{Path: path, Source: src})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
