// Package schemajson is a small, dependency-free validator for the subset of
// JSON Schema (draft 2020-12) used by the published block schema
// (docs/schema/block.schema.json). It exists to back the AS-061 divergence
// guard: every document the Go reference implementation (package schema)
// marshals must validate against the published schema, and a curated corpus of
// invalid documents must be rejected — so the schema actually constrains and a
// Go change that breaks an invariant fails CI.
//
// It is deliberately NOT a general-purpose validator. It implements only the
// keywords the block schema uses: $ref/$defs, type, properties, required,
// additionalProperties, items, enum, const, minimum, allOf, and if/then/else.
// Honoring the additive-only discipline (PRD D2), the block schema leaves
// additionalProperties open and uses no closed enums, so unknown fields and
// unknown block kinds validate.
package schemajson

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// Schema is a compiled JSON Schema ready to validate instances against.
type Schema struct {
	root map[string]any
}

// Compile parses a JSON Schema document. The schema's keywords are interpreted
// lazily at Validate time; Compile only checks that the bytes are valid JSON
// describing an object.
func Compile(data []byte) (*Schema, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("schemajson: parse schema: %w", err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schemajson: schema root must be a JSON object")
	}
	return &Schema{root: obj}, nil
}

// Validate reports whether the JSON document data conforms to the schema. It
// returns nil on success, or an error describing every constraint the instance
// violates (one per line), so a failing guard test points straight at the gap.
func (s *Schema) Validate(data []byte) error {
	var inst any
	if err := json.Unmarshal(data, &inst); err != nil {
		return fmt.Errorf("schemajson: parse instance: %w", err)
	}
	v := &validator{root: s.root}
	v.validate(s.root, inst, "")
	if len(v.errs) == 0 {
		return nil
	}
	sort.Strings(v.errs)
	return fmt.Errorf("schemajson: %d validation error(s):\n  %s", len(v.errs), strings.Join(v.errs, "\n  "))
}

type validator struct {
	root map[string]any
	errs []string
}

func (v *validator) fail(path, format string, args ...any) {
	at := path
	if at == "" {
		at = "(root)"
	}
	v.errs = append(v.errs, at+": "+fmt.Sprintf(format, args...))
}

// validate applies every supported keyword in sch to inst at the given path.
func (v *validator) validate(sch map[string]any, inst any, path string) {
	if ref, ok := sch["$ref"].(string); ok {
		target := v.resolve(ref)
		if target == nil {
			v.fail(path, "unresolved $ref %q", ref)
			return
		}
		// The block schema never places sibling keywords next to $ref, so
		// delegating wholly to the target is sufficient.
		v.validate(target, inst, path)
		return
	}

	if t, ok := sch["type"]; ok {
		v.checkType(t, inst, path)
	}
	if c, ok := sch["const"]; ok {
		if !deepEqual(c, inst) {
			v.fail(path, "must equal const %v", c)
		}
	}
	if e, ok := sch["enum"].([]any); ok {
		matched := false
		for _, want := range e {
			if deepEqual(want, inst) {
				matched = true
				break
			}
		}
		if !matched {
			v.fail(path, "must be one of enum %v", e)
		}
	}
	if m, ok := sch["minimum"]; ok {
		if n, isNum := inst.(float64); isNum {
			if min, isNum2 := toFloat(m); isNum2 && n < min {
				v.fail(path, "must be >= %v", m)
			}
		}
	}
	if obj, ok := inst.(map[string]any); ok {
		v.checkObject(sch, obj, path)
	}
	if arr, ok := inst.([]any); ok {
		if items, ok := sch["items"].(map[string]any); ok {
			for i, el := range arr {
				v.validate(items, el, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	if all, ok := sch["allOf"].([]any); ok {
		for _, sub := range all {
			if subSch, ok := sub.(map[string]any); ok {
				v.validate(subSch, inst, path)
			}
		}
	}
	if ifSch, ok := sch["if"].(map[string]any); ok {
		if v.matches(ifSch, inst) {
			if thenSch, ok := sch["then"].(map[string]any); ok {
				v.validate(thenSch, inst, path)
			}
		} else if elseSch, ok := sch["else"].(map[string]any); ok {
			v.validate(elseSch, inst, path)
		}
	}
}

// checkObject applies the object keywords: required, properties, and
// additionalProperties.
func (v *validator) checkObject(sch map[string]any, obj map[string]any, path string) {
	if req, ok := sch["required"].([]any); ok {
		for _, r := range req {
			if key, ok := r.(string); ok {
				if _, present := obj[key]; !present {
					v.fail(path, "missing required property %q", key)
				}
			}
		}
	}
	props, _ := sch["properties"].(map[string]any)
	for key, val := range obj {
		if propSch, ok := props[key].(map[string]any); ok {
			v.validate(propSch, val, joinPath(path, key))
		} else if ap, ok := sch["additionalProperties"].(bool); ok && !ap {
			v.fail(path, "additional property %q not allowed", key)
		}
	}
}

// matches reports whether inst validates against sch with no errors. Used for
// the "if" clause, whose failures must not be reported as instance errors.
func (v *validator) matches(sch map[string]any, inst any) bool {
	probe := &validator{root: v.root}
	probe.validate(sch, inst, "")
	return len(probe.errs) == 0
}

// resolve follows a local JSON pointer of the form "#/$defs/Name".
func (v *validator) resolve(ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	cur := any(v.root)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[part]
	}
	out, _ := cur.(map[string]any)
	return out
}

// checkType enforces the "type" keyword, accepting a single type or a list of
// permitted types.
func (v *validator) checkType(t, inst any, path string) {
	switch tv := t.(type) {
	case string:
		if !jsonTypeMatches(tv, inst) {
			v.fail(path, "must be of type %q, got %s", tv, jsonTypeOf(inst))
		}
	case []any:
		for _, alt := range tv {
			if s, ok := alt.(string); ok && jsonTypeMatches(s, inst) {
				return
			}
		}
		v.fail(path, "must be one of types %v, got %s", tv, jsonTypeOf(inst))
	}
}

func jsonTypeMatches(t string, inst any) bool {
	switch t {
	case "object":
		_, ok := inst.(map[string]any)
		return ok
	case "array":
		_, ok := inst.([]any)
		return ok
	case "string":
		_, ok := inst.(string)
		return ok
	case "boolean":
		_, ok := inst.(bool)
		return ok
	case "number":
		_, ok := inst.(float64)
		return ok
	case "integer":
		n, ok := inst.(float64)
		return ok && n == math.Trunc(n)
	case "null":
		return inst == nil
	}
	return false
}

func jsonTypeOf(inst any) string {
	switch n := inst.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if n == math.Trunc(n) {
			return "integer/number"
		}
		return "number"
	case nil:
		return "null"
	}
	return "unknown"
}

func toFloat(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// deepEqual compares two decoded-JSON values. Numbers both decode to float64
// from json.Unmarshal, so reflect.DeepEqual is exact enough here.
func deepEqual(a, b any) bool { return reflect.DeepEqual(a, b) }
