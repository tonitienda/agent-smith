package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// layer is a tiny helper to build a Layer from a JSON literal in tests.
func jsonLayer(t *testing.T, band string, body string) Layer {
	t.Helper()
	path := filepath.Join(t.TempDir(), band+".json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	l, err := FileLayer(band, path)
	if err != nil {
		t.Fatalf("FileLayer %s: %v", band, err)
	}
	return l
}

func TestScalarPrecedence(t *testing.T) {
	// default < user < project < flag; the highest layer that sets "model" wins.
	c := New(
		jsonLayer(t, "default", `{"model":"default-model"}`),
		jsonLayer(t, "user", `{"model":"user-model"}`),
		jsonLayer(t, "project", `{"model":"project-model"}`),
		MapLayer("flag", "flags", map[string]any{"model": "flag-model"}),
	)

	v, src, ok := c.String("model")
	if !ok || v != "flag-model" || src.Layer != "flag" {
		t.Fatalf("model = %q %v %v; want flag-model/flag/true", v, src.Layer, ok)
	}

	// Drop the flag layer: project should win next, then user, then default.
	c = New(
		jsonLayer(t, "default", `{"model":"default-model"}`),
		jsonLayer(t, "user", `{"model":"user-model"}`),
		jsonLayer(t, "project", `{"model":"project-model"}`),
	)
	if v, src, _ := c.String("model"); v != "project-model" || src.Layer != "project" {
		t.Fatalf("after dropping flag: %q/%s; want project-model/project", v, src.Layer)
	}
	c = New(
		jsonLayer(t, "default", `{"model":"default-model"}`),
		jsonLayer(t, "user", `{"model":"user-model"}`),
	)
	if v, src, _ := c.String("model"); v != "user-model" || src.Layer != "user" {
		t.Fatalf("user layer: %q/%s; want user-model/user", v, src.Layer)
	}
	c = New(jsonLayer(t, "default", `{"model":"default-model"}`))
	if v, src, _ := c.String("model"); v != "default-model" || src.Layer != "default" {
		t.Fatalf("default layer: %q/%s; want default-model/default", v, src.Layer)
	}
}

func TestMapDeepMerge(t *testing.T) {
	// Nested objects merge key by key: project overrides one leaf, user keeps the
	// other, and each leaf remembers the layer that set it.
	c := New(
		jsonLayer(t, "user", `{"subagents":{"insights":{"enabled":true,"model":"cheap"}}}`),
		jsonLayer(t, "project", `{"subagents":{"insights":{"model":"smart"}}}`),
	)

	if v, src, ok := c.Bool("subagents.insights.enabled"); !ok || !v || src.Layer != "user" {
		t.Fatalf("enabled = %v %s %v; want true/user/true", v, src.Layer, ok)
	}
	if v, src, ok := c.String("subagents.insights.model"); !ok || v != "smart" || src.Layer != "project" {
		t.Fatalf("model = %q %s %v; want smart/project/true", v, src.Layer, ok)
	}
}

func TestListReplaces(t *testing.T) {
	// Lists override wholesale (a higher layer's list wins, not concatenation).
	c := New(
		jsonLayer(t, "user", `{"tools":["read","write","shell"]}`),
		jsonLayer(t, "project", `{"tools":["read"]}`),
	)
	v, src, ok := c.Strings("tools")
	if !ok || src.Layer != "project" || !reflect.DeepEqual(v, []string{"read"}) {
		t.Fatalf("tools = %v %s %v; want [read]/project/true", v, src.Layer, ok)
	}
}

func TestLeafReplacedByObjectAndBack(t *testing.T) {
	// A higher layer may replace a scalar with an object: the old leaf's source
	// must not linger, and the new leaves must resolve.
	c := New(
		jsonLayer(t, "user", `{"x":"scalar"}`),
		jsonLayer(t, "project", `{"x":{"y":1}}`),
	)
	if _, _, ok := c.String("x"); ok {
		t.Fatal("x should no longer be a scalar leaf")
	}
	if v, src, ok := c.Int("x.y"); !ok || v != 1 || src.Layer != "project" {
		t.Fatalf("x.y = %v %s %v; want 1/project/true", v, src.Layer, ok)
	}

	// And the reverse: an object replaced by a scalar clears descendant sources.
	c = New(
		jsonLayer(t, "user", `{"x":{"y":1}}`),
		jsonLayer(t, "project", `{"x":"scalar"}`),
	)
	if _, _, ok := c.Int("x.y"); ok {
		t.Fatal("x.y should be gone after x became a scalar")
	}
	if v, _, ok := c.String("x"); !ok || v != "scalar" {
		t.Fatalf("x = %q %v; want scalar/true", v, ok)
	}
	if got := c.Effective(); len(got) != 1 || got[0].Path != "x" {
		t.Fatalf("Effective = %+v; want single leaf x", got)
	}
}

func TestTypedAccessors(t *testing.T) {
	c := New(jsonLayer(t, "user", `{"s":"hi","b":true,"i":7,"f":1.5,"list":["a","b"]}`))

	if v, _, ok := c.String("s"); !ok || v != "hi" {
		t.Fatalf("String: %q %v", v, ok)
	}
	if v, _, ok := c.Bool("b"); !ok || !v {
		t.Fatalf("Bool: %v %v", v, ok)
	}
	if v, _, ok := c.Int("i"); !ok || v != 7 {
		t.Fatalf("Int: %v %v", v, ok)
	}
	if v, _, ok := c.Float64("f"); !ok || v != 1.5 {
		t.Fatalf("Float64: %v %v", v, ok)
	}
	if v, _, ok := c.Strings("list"); !ok || !reflect.DeepEqual(v, []string{"a", "b"}) {
		t.Fatalf("Strings: %v %v", v, ok)
	}

	// In-memory MapLayer values (env/flag overrides) use native Go types, not
	// JSON's float64/[]any — the accessors must resolve those too.
	cMap := New(MapLayer("flag", "flags", map[string]any{
		"i":    7,
		"i64":  int64(9),
		"f32":  float32(2.5),
		"list": []string{"a", "b"},
	}))
	if v, _, ok := cMap.Int("i"); !ok || v != 7 {
		t.Fatalf("MapLayer Int: %v %v", v, ok)
	}
	if v, _, ok := cMap.Int("i64"); !ok || v != 9 {
		t.Fatalf("MapLayer Int64: %v %v", v, ok)
	}
	if v, _, ok := cMap.Float64("f32"); !ok || v != 2.5 {
		t.Fatalf("MapLayer Float32: %v %v", v, ok)
	}
	if v, _, ok := cMap.Strings("list"); !ok || !reflect.DeepEqual(v, []string{"a", "b"}) {
		t.Fatalf("MapLayer Strings: %v %v", v, ok)
	}

	// Wrong-type and missing lookups report ok=false rather than panicking.
	if _, _, ok := c.Int("f"); ok {
		t.Fatal("1.5 should not read as Int")
	}
	if _, _, ok := c.String("b"); ok {
		t.Fatal("bool should not read as String")
	}
	if _, _, ok := c.String("missing"); ok {
		t.Fatal("missing key should be ok=false")
	}
	if _, _, ok := c.Strings("list-of-numbers"); ok {
		t.Fatal("absent list ok=false")
	}
}

func TestUnknownKeysWarnNamingFileAndKey(t *testing.T) {
	l := jsonLayer(t, "user", `{"model":"x","mdoel":"typo","subagents":{"a":1}}`)
	c := New(l)

	warns := c.Unknown("model", "subagents")
	if len(warns) != 1 {
		t.Fatalf("warnings = %+v; want exactly one (mdoel)", warns)
	}
	w := warns[0]
	if w.Path != "mdoel" {
		t.Fatalf("warning path = %q; want mdoel", w.Path)
	}
	if w.Source.Origin != l.Source.Origin {
		t.Fatalf("warning origin = %q; want the file path %q", w.Source.Origin, l.Source.Origin)
	}
	// Preserved despite the warning (forward-compat).
	if v, _, ok := c.String("mdoel"); !ok || v != "typo" {
		t.Fatalf("unknown key not preserved: %q %v", v, ok)
	}
}

func TestUnknownNestedSectionWarnsOnce(t *testing.T) {
	// An unknown top-level section with many leaves yields a single warning
	// naming the section, not one per leaf.
	c := New(jsonLayer(t, "user", `{"bogus":{"a":1,"b":2,"c":{"d":3}}}`))
	warns := c.Unknown("model")
	if len(warns) != 1 {
		t.Fatalf("warnings = %+v; want exactly one for the bogus section", warns)
	}
	if warns[0].Path != "bogus" {
		t.Fatalf("warning path = %q; want the section name bogus", warns[0].Path)
	}
}

func TestMissingFileIsEmptyLayer(t *testing.T) {
	l, err := FileLayer("user", filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if l.Values != nil {
		t.Fatalf("missing file should yield nil Values, got %v", l.Values)
	}
	if got := New(l).Effective(); len(got) != 0 {
		t.Fatalf("empty layer should resolve nothing, got %+v", got)
	}
}

func TestBadJSONErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FileLayer("user", path); err == nil {
		t.Fatal("malformed JSON should error")
	}
}

func TestEffectiveSorted(t *testing.T) {
	c := New(jsonLayer(t, "user", `{"b":2,"a":{"y":1,"x":2}}`))
	got := c.Effective()
	want := []string{"a.x", "a.y", "b"}
	if len(got) != len(want) {
		t.Fatalf("Effective len = %d; want %d (%+v)", len(got), len(want), got)
	}
	for i, p := range want {
		if got[i].Path != p {
			t.Fatalf("Effective[%d].Path = %q; want %q", i, got[i].Path, p)
		}
	}
}

func TestValueLeafAndSection(t *testing.T) {
	c := New(
		jsonLayer(t, "user", `{"model":"user-model","permissions":{"default_mode":"ask"}}`),
		jsonLayer(t, "project", `{"permissions":{"default_mode":"allowlist"}}`),
	)

	// A leaf resolves with the winning layer's source.
	v, src, ok := c.Value("permissions.default_mode")
	if !ok || v != "allowlist" || src.Layer != "project" {
		t.Fatalf("Value(leaf) = (%v, %q, %v); want (allowlist, project, true)", v, src.Layer, ok)
	}

	// An interior section resolves as the merged subtree with an empty source.
	v, src, ok = c.Value("permissions")
	if !ok || src.Layer != "" {
		t.Fatalf("Value(section) ok=%v source=%q; want ok=true, empty source", ok, src.Layer)
	}
	if m, isMap := v.(map[string]any); !isMap || m["default_mode"] != "allowlist" {
		t.Fatalf("Value(section) = %#v; want merged map with default_mode=allowlist", v)
	}

	// An unset path reports ok=false.
	if _, _, ok := c.Value("nope"); ok {
		t.Error("Value(unset) ok=true; want false")
	}

	// Empty path segments are rejected on read too (symmetry with SetFileValue).
	for _, p := range []string{"permissions..default_mode", ".permissions", "permissions."} {
		if _, _, ok := c.Value(p); ok {
			t.Errorf("Value(%q) ok=true; want false (empty segment)", p)
		}
	}
}

func TestDecodeSection(t *testing.T) {
	c := New(
		jsonLayer(t, "user", `{"pricing":{"version":1,"currency":"USD"}}`),
		jsonLayer(t, "project", `{"pricing":{"currency":"EUR"}}`),
	)
	var got struct {
		Version  int    `json:"version"`
		Currency string `json:"currency"`
	}
	ok, err := c.Decode("pricing", &got)
	if err != nil || !ok {
		t.Fatalf("Decode = (%v, %v); want (true, nil)", ok, err)
	}
	if got.Version != 1 || got.Currency != "EUR" {
		t.Fatalf("Decode merged = %+v; want version=1 currency=EUR", got)
	}

	// An unset path leaves the target untouched and reports ok=false.
	var unused struct{ X int }
	if ok, err := c.Decode("absent", &unused); ok || err != nil {
		t.Fatalf("Decode(absent) = (%v, %v); want (false, nil)", ok, err)
	}
}

func TestSetFileValueRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	// Create on first write, then a nested write must preserve the first key.
	if err := SetFileValue(path, "model", "claude-opus-4-8"); err != nil {
		t.Fatalf("SetFileValue(model): %v", err)
	}
	if err := SetFileValue(path, "permissions.default_mode", "allowlist"); err != nil {
		t.Fatalf("SetFileValue(nested): %v", err)
	}

	l, err := FileLayer("project", path)
	if err != nil {
		t.Fatalf("FileLayer: %v", err)
	}
	c := New(l)
	if v, _, ok := c.Value("model"); !ok || v != "claude-opus-4-8" {
		t.Errorf("model = %v (ok=%v); want claude-opus-4-8", v, ok)
	}
	if v, _, ok := c.Value("permissions.default_mode"); !ok || v != "allowlist" {
		t.Errorf("permissions.default_mode = %v (ok=%v); want allowlist", v, ok)
	}
}

func TestSetFileValueRejectsEmptySegment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, key := range []string{"", "a..b", ".a", "a."} {
		if err := SetFileValue(path, key, "v"); err == nil {
			t.Errorf("SetFileValue(%q) accepted an empty path segment", key)
		}
	}
	// A rejected write must not have created the file.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("rejected write created the file: stat err = %v", err)
	}
}
