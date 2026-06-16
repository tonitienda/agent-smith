package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// validateArgs checks a model's raw arguments against the slice of JSON Schema
// this validator understands, returning a model-readable error describing the
// first problem it finds, or nil when the arguments are acceptable.
//
// Coverage (AS-013 + AS-062): an object schema's required properties, declared
// property types (scalar, array, object, and union "type" arrays), and — when
// additionalProperties is false — the absence of unexpected properties; plus,
// recursively into nested object properties and array items, enum membership,
// numeric minimum/maximum, string minLength/maxLength/pattern, and array
// minItems/maxItems. The validator stays deliberately lenient: anything it does
// not model (composition keywords like oneOf/anyOf/allOf, format, an
// uncompilable pattern, a boolean or non-object subschema) is ignored rather
// than rejected, so it never punishes a call it cannot fully understand. It
// turns the clearly-wrong cases into a message the model can act on; it is not a
// conformant JSON Schema implementation.
func validateArgs(schemaJSON, args json.RawMessage) error {
	s, ok := parseSchema(schemaJSON)
	if !ok {
		// No schema, or one the validator cannot model (malformed, boolean,
		// non-object root): accept any arguments rather than guess.
		return nil
	}
	if len(s.Types) > 0 && !anyTypeMatches(s.Types, "object") {
		return nil // we only validate object-shaped argument schemas
	}

	fields, err := argObject(args)
	if err != nil {
		return err
	}
	return s.checkObject(fields, "")
}

// schemaNode is the slice of a (possibly nested) JSON Schema this validator
// understands. Subschemas are kept as raw JSON and parsed lazily by parseSchema,
// so an unmodelable subschema (a boolean schema, a tuple-form "items" array)
// degrades to "skip" instead of failing the whole parse.
type schemaNode struct {
	Types                []string
	Required             []string
	Properties           map[string]json.RawMessage
	AdditionalProperties json.RawMessage
	Items                json.RawMessage
	Enum                 []json.RawMessage
	Minimum              *float64
	Maximum              *float64
	MinLength            *int
	MaxLength            *int
	Pattern              string
	MinItems             *int
	MaxItems             *int
}

// UnmarshalJSON reads the supported keywords, normalizing "type" — which may be
// a scalar string or an array of strings (a union) — into Types.
func (s *schemaNode) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type                 json.RawMessage            `json:"type"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties json.RawMessage            `json:"additionalProperties"`
		Items                json.RawMessage            `json:"items"`
		Enum                 []json.RawMessage          `json:"enum"`
		Minimum              *float64                   `json:"minimum"`
		Maximum              *float64                   `json:"maximum"`
		MinLength            *int                       `json:"minLength"`
		MaxLength            *int                       `json:"maxLength"`
		Pattern              string                     `json:"pattern"`
		MinItems             *int                       `json:"minItems"`
		MaxItems             *int                       `json:"maxItems"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if single := ""; json.Unmarshal(raw.Type, &single) == nil && single != "" {
		s.Types = []string{single}
	} else {
		var arr []string
		if json.Unmarshal(raw.Type, &arr) == nil {
			s.Types = arr
		}
	}
	s.Required = raw.Required
	s.Properties = raw.Properties
	s.AdditionalProperties = raw.AdditionalProperties
	s.Items = raw.Items
	s.Enum = raw.Enum
	s.Minimum, s.Maximum = raw.Minimum, raw.Maximum
	s.MinLength, s.MaxLength = raw.MinLength, raw.MaxLength
	s.Pattern = raw.Pattern
	s.MinItems, s.MaxItems = raw.MinItems, raw.MaxItems
	return nil
}

// parseSchema decodes a (sub)schema, reporting ok=false when it is empty, a
// boolean schema, a non-object value, or malformed — all cases the caller should
// treat leniently (skip) rather than reject.
func parseSchema(raw json.RawMessage) (*schemaNode, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false
	}
	var s schemaNode
	if json.Unmarshal(raw, &s) != nil {
		return nil, false
	}
	return &s, true
}

// checkObject validates an object's fields against this schema: required
// properties present, no unexpected properties when additionalProperties is
// false, and each declared property recursively. base is the dotted path of the
// containing object ("" at the argument root), so error messages name the full
// property path.
func (s *schemaNode) checkObject(fields map[string]json.RawMessage, base string) error {
	for _, req := range s.Required {
		if _, ok := fields[req]; !ok {
			return fmt.Errorf("missing required property %q", joinPath(base, req))
		}
	}
	if s.additionalPropertiesForbidden() {
		for _, name := range sortedKeys(fields) {
			if _, declared := s.Properties[name]; !declared {
				return fmt.Errorf("unexpected property %q", joinPath(base, name))
			}
		}
	}
	for _, name := range sortedKeys(fields) {
		propRaw, declared := s.Properties[name]
		if !declared {
			continue
		}
		if err := validateValue(fields[name], propRaw, joinPath(base, name)); err != nil {
			return err
		}
	}
	return nil
}

// validateValue checks a single value against schemaRaw at the given path. It
// enforces type and enum, then recurses into the keywords relevant to the
// value's JSON type. An unmodelable schema or indeterminate value is skipped.
func validateValue(value, schemaRaw json.RawMessage, path string) error {
	s, ok := parseSchema(schemaRaw)
	if !ok {
		return nil
	}
	got := jsonType(value)
	if got == "" {
		return nil
	}
	if len(s.Types) > 0 && !anyTypeMatches(s.Types, got) {
		return fmt.Errorf("property %q must be %s, got %s", path, strings.Join(s.Types, " or "), got)
	}
	if len(s.Enum) > 0 && !enumContains(s.Enum, value) {
		return fmt.Errorf("property %q must be one of %s", path, enumList(s.Enum))
	}
	switch got {
	case "object":
		var fields map[string]json.RawMessage
		if json.Unmarshal(value, &fields) != nil {
			return nil
		}
		return s.checkObject(fields, path)
	case "array":
		return s.checkArray(value, path)
	case "string":
		return s.checkString(value, path)
	case "integer", "number":
		return s.checkNumber(value, path)
	}
	return nil
}

// checkArray enforces minItems/maxItems and validates each element against the
// "items" subschema.
func (s *schemaNode) checkArray(value json.RawMessage, path string) error {
	var elems []json.RawMessage
	if json.Unmarshal(value, &elems) != nil {
		return nil
	}
	if s.MinItems != nil && len(elems) < *s.MinItems {
		return fmt.Errorf("property %q must have at least %d items, got %d", path, *s.MinItems, len(elems))
	}
	if s.MaxItems != nil && len(elems) > *s.MaxItems {
		return fmt.Errorf("property %q must have at most %d items, got %d", path, *s.MaxItems, len(elems))
	}
	if len(bytes.TrimSpace(s.Items)) > 0 {
		for i, el := range elems {
			if err := validateValue(el, s.Items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkString enforces minLength/maxLength (in Unicode code points) and pattern.
// An uncompilable pattern is treated as unmodeled and skipped.
func (s *schemaNode) checkString(value json.RawMessage, path string) error {
	var str string
	if json.Unmarshal(value, &str) != nil {
		return nil
	}
	n := utf8.RuneCountInString(str)
	if s.MinLength != nil && n < *s.MinLength {
		return fmt.Errorf("property %q must be at least %d characters, got %d", path, *s.MinLength, n)
	}
	if s.MaxLength != nil && n > *s.MaxLength {
		return fmt.Errorf("property %q must be at most %d characters, got %d", path, *s.MaxLength, n)
	}
	if s.Pattern != "" {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil
		}
		if !re.MatchString(str) {
			return fmt.Errorf("property %q must match pattern %q", path, s.Pattern)
		}
	}
	return nil
}

// checkNumber enforces minimum/maximum.
func (s *schemaNode) checkNumber(value json.RawMessage, path string) error {
	var n float64
	if json.Unmarshal(value, &n) != nil {
		return nil
	}
	if s.Minimum != nil && n < *s.Minimum {
		return fmt.Errorf("property %q must be >= %v, got %v", path, *s.Minimum, n)
	}
	if s.Maximum != nil && n > *s.Maximum {
		return fmt.Errorf("property %q must be <= %v, got %v", path, *s.Maximum, n)
	}
	return nil
}

// additionalPropertiesForbidden reports whether the schema sets
// "additionalProperties": false. The keyword may also be a schema object; we
// only honor the boolean-false form.
func (s schemaNode) additionalPropertiesForbidden() bool {
	var b bool
	return json.Unmarshal(s.AdditionalProperties, &b) == nil && !b
}

// enumContains reports whether value JSON-equals one of the enum members. Values
// are compared by decoded shape, so 3 and 3.0 match.
func enumContains(enum []json.RawMessage, value json.RawMessage) bool {
	var want any
	if json.Unmarshal(value, &want) != nil {
		return true // indeterminate: don't reject
	}
	for _, e := range enum {
		var got any
		if json.Unmarshal(e, &got) == nil && reflect.DeepEqual(got, want) {
			return true
		}
	}
	return false
}

// enumList renders the allowed values for an error message.
func enumList(enum []json.RawMessage) string {
	parts := make([]string, len(enum))
	for i, e := range enum {
		parts[i] = strings.TrimSpace(string(e))
	}
	return strings.Join(parts, ", ")
}

// anyTypeMatches reports whether got satisfies any of the declared types (so a
// union "type" array passes when the value matches one member).
func anyTypeMatches(want []string, got string) bool {
	for _, w := range want {
		if typeMatches(w, got) {
			return true
		}
	}
	return false
}

// joinPath builds a dotted property path, used so nested errors name the full
// location (e.g. "filter.kind"). The root object has an empty base.
func joinPath(base, name string) string {
	if base == "" {
		return name
	}
	return base + "." + name
}

// argObject decodes args as a JSON object's fields. Empty/absent arguments are
// treated as an empty object; a non-object payload is a model error.
func argObject(args json.RawMessage) (map[string]json.RawMessage, error) {
	args = bytes.TrimSpace(args)
	if len(args) == 0 || bytes.Equal(args, []byte("null")) {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	return fields, nil
}

// jsonType reports the JSON type of a raw value as a JSON Schema type name, or
// "" if it cannot be determined. A number with no fractional part and no
// exponent reports "integer" so it satisfies both "integer" and "number".
func jsonType(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		if bytes.ContainsAny(raw, ".eE") {
			return "number"
		}
		return "integer"
	}
}

// typeMatches reports whether an observed JSON type satisfies a schema-declared
// type. An "integer" value also satisfies a "number" requirement.
func typeMatches(want, got string) bool {
	if want == got {
		return true
	}
	return want == "number" && got == "integer"
}

// sortedKeys returns the map keys in deterministic order so validation reports
// the same first error for the same input.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
