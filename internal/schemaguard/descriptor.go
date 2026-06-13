// Package schemaguard mechanically enforces the additive-only schema promise
// (PRD D2): from V1 the content-block schema (package schema, AS-003) is
// additive-only forever — no field is ever removed, renamed, retyped, or
// repurposed, and no enum value is dropped. New concepts arrive only as new
// optional fields, new struct types, or new enum values.
//
// The guard works by reflecting the Go schema types into a flat, serializable
// Descriptor and diffing the current descriptor against a committed baseline
// (testdata/schema_baseline.json). A removed/renamed/retyped field or a dropped
// enum value is a breaking change and fails the diff (and therefore CI, via
// `go test ./...`). Additions never fail. The companion cmd/schema-guard tool
// regenerates the baseline and golden corpus with -update; see
// docs/schema/EVOLUTION.md for the contributor process.
package schemaguard

import (
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/schema"
)

// Field describes one struct field of the schema as it appears on the wire.
// Name is the Go field name (the stable identity used to detect renames); JSON
// is the serialized key; Type is a canonical, package-stable type string; and
// OmitEmpty records wire-presence semantics. A change to any of these on an
// existing field is a breaking change.
type Field struct {
	Name      string `json:"name"`
	JSON      string `json:"json"`
	Type      string `json:"type"`
	OmitEmpty bool   `json:"omitempty"`
}

// Type describes one struct type reachable from the schema roots.
type Type struct {
	Fields []Field `json:"fields"`
}

// Descriptor is the full, serializable shape of the schema: every struct type
// reachable from the roots and every named enum and its permitted values. It is
// the unit of comparison for the additive-only diff.
type Descriptor struct {
	Types map[string]Type     `json:"types"`
	Enums map[string][]string `json:"enums"`
}

// schemaPkgPath is the import path of the reference-implementation package whose
// structs the guard describes. Types from other packages (time.Time,
// json.RawMessage) are treated as opaque leaves and named by their type string.
var schemaPkgPath = reflect.TypeOf(schema.Block{}).PkgPath()

// Generate reflects the current schema types and enum registry into a
// Descriptor. It walks every struct reachable from Document and Block, so any
// type wired into the schema is captured automatically.
func Generate() Descriptor {
	d := Descriptor{Types: map[string]Type{}, Enums: enumRegistry()}

	queue := []reflect.Type{
		reflect.TypeOf(schema.Document{}),
		reflect.TypeOf(schema.Block{}),
	}
	for len(queue) > 0 {
		t := deref(queue[0])
		queue = queue[1:]
		if t.Kind() != reflect.Struct || t.PkgPath() != schemaPkgPath {
			continue // not one of our structs (e.g. time.Time): leaf
		}
		name := t.Name()
		if _, done := d.Types[name]; done {
			continue
		}

		var fields []Field
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // unexported
			}
			jsonName, omit, skip := parseJSONTag(f)
			if skip {
				continue
			}
			fields = append(fields, Field{
				Name:      f.Name,
				JSON:      jsonName,
				Type:      typeString(f.Type),
				OmitEmpty: omit,
			})
			enqueueStructTypes(f.Type, &queue)
		}
		d.Types[name] = Type{Fields: fields}
	}
	return d
}

// enumRegistry lists every named enum in the schema and its permitted values,
// sourced from the schema constants so the registry cannot silently drift from
// the implementation (removing a constant fails to compile here). Dropping a
// value from the baseline is caught by the diff.
func enumRegistry() map[string][]string {
	return map[string][]string{
		"Kind": {
			string(schema.KindText),
			string(schema.KindToolCall),
			string(schema.KindToolResult),
			string(schema.KindFileRead),
			string(schema.KindReasoning),
			string(schema.KindCompaction),
			string(schema.KindFallback),
		},
		"Role": {
			string(schema.RoleUser),
			string(schema.RoleAssistant),
			string(schema.RoleSystem),
			string(schema.RoleTool),
			string(schema.RoleHarness),
		},
		"ToolKind":    {schema.ToolKindClient, schema.ToolKindServer},
		"TextSubtype": {schema.TextSubtypeNormal, schema.TextSubtypeRefusal},
		"ReplayScope": {schema.ReplaySameModelOnly, schema.ReplayPortable},
		"CacheMode":   {schema.CacheModeExplicit, schema.CacheModeAutomatic},
		"FileSource":  {schema.FileSourceTool, schema.FileSourceAttachment, schema.FileSourceMCPResource},
	}
}

// deref unwraps a pointer type to its element; other types are returned as-is.
func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// enqueueStructTypes appends any struct types reachable through pointers,
// slices, arrays, and map values of t to the work queue, so nested schema
// structs are discovered and described.
func enqueueStructTypes(t reflect.Type, queue *[]reflect.Type) {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array:
		enqueueStructTypes(t.Elem(), queue)
	case reflect.Map:
		enqueueStructTypes(t.Elem(), queue)
	case reflect.Struct:
		*queue = append(*queue, t)
	}
}

// typeString renders a canonical, stable string for a field type. Schema
// package types are named bare (e.g. Tokens, []Part, *LineRange); basic types
// use their kind name; foreign types keep a short package qualifier
// (json.RawMessage, time.Time). The string is the comparison key for detecting
// type changes, so it must be deterministic across builds.
func typeString(t reflect.Type) string {
	// Named non-basic types — our enums/structs plus json.RawMessage and
	// time.Time — are rendered by name first, so their meaning (not merely
	// their []byte/struct representation) is part of the recorded contract.
	if t.Name() != "" && t.PkgPath() != "" {
		if t.PkgPath() == schemaPkgPath {
			return t.Name() // our own named types: bare
		}
		return path.Base(t.PkgPath()) + "." + t.Name()
	}
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + typeString(t.Elem())
	case reflect.Slice:
		return "[]" + typeString(t.Elem())
	case reflect.Map:
		return "map[" + typeString(t.Key()) + "]" + typeString(t.Elem())
	}
	if t.Name() != "" {
		return t.Name() // basic types: string, int, bool, float64
	}
	return t.String()
}

// parseJSONTag extracts the wire name and omitempty flag from a field's json
// tag, following encoding/json semantics: an empty/absent tag name defaults to
// the Go field name, and a "-" name means the field is not serialized.
func parseJSONTag(f reflect.StructField) (name string, omitempty, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = f.Name
	}
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, false
}

// sortedKeys returns the keys of a string-keyed map in sorted order, for
// deterministic iteration in diffs and messages.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
