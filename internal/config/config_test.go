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
