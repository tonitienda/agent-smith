package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// validateArgs checks a model's raw arguments against a pragmatic subset of the
// tool's JSON Schema, returning a model-readable error describing the first
// problem it finds, or nil when the arguments are acceptable.
//
// The subset is deliberately small and lenient: it validates that an object
// schema's required properties are present, that declared scalar/array/object
// property types match, and — when additionalProperties is false — that no
// unexpected property is supplied. Anything the subset does not model (nested
// schemas, enums, formats, numeric bounds, composition keywords) is ignored, so
// the validator never rejects a call it cannot fully understand. It exists to
// turn the common, clearly-wrong cases (a missing required field, a string where
// a number was asked for) into a message the model can act on, not to be a
// conformant JSON Schema implementation. Fuller validation is deferred (AS-062).
func validateArgs(schemaJSON, args json.RawMessage) error {
	schemaJSON = bytes.TrimSpace(schemaJSON)
	if len(schemaJSON) == 0 {
		return nil // no schema declared: accept any arguments
	}

	var s miniSchema
	if err := json.Unmarshal(schemaJSON, &s); err != nil {
		// A malformed schema is a tool-authoring bug, not a model mistake. Don't
		// punish the model for it: skip validation rather than reject the call.
		return nil
	}
	if s.Type != "" && s.Type != "object" {
		return nil // we only validate object-shaped argument schemas
	}

	fields, err := argObject(args)
	if err != nil {
		return err
	}

	for _, req := range s.Required {
		if _, ok := fields[req]; !ok {
			return fmt.Errorf("missing required property %q", req)
		}
	}

	if s.additionalPropertiesForbidden() {
		for _, name := range sortedKeys(fields) {
			if _, declared := s.Properties[name]; !declared {
				return fmt.Errorf("unexpected property %q", name)
			}
		}
	}

	for _, name := range sortedKeys(fields) {
		prop, declared := s.Properties[name]
		if !declared || prop.Type == "" {
			continue // undeclared or untyped: nothing to check
		}
		if got := jsonType(fields[name]); got != "" && !typeMatches(prop.Type, got) {
			return fmt.Errorf("property %q must be %s, got %s", name, prop.Type, got)
		}
	}
	return nil
}

// miniSchema is the slice of JSON Schema this validator understands.
type miniSchema struct {
	Type                 string                `json:"type"`
	Required             []string              `json:"required"`
	Properties           map[string]propSchema `json:"properties"`
	AdditionalProperties json.RawMessage       `json:"additionalProperties"`
}

// propSchema is the slice of a property's schema this validator understands. A
// property whose "type" is absent or an array (a union type) leaves Type empty
// and is not type-checked.
type propSchema struct {
	Type string `json:"-"`
}

// UnmarshalJSON reads only a scalar string "type", tolerating (and ignoring) the
// array form and any other keywords.
func (p *propSchema) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var t string
	if json.Unmarshal(raw.Type, &t) == nil {
		p.Type = t
	}
	return nil
}

// additionalPropertiesForbidden reports whether the schema sets
// "additionalProperties": false. The keyword may also be a schema object; we
// only honor the boolean-false form.
func (s miniSchema) additionalPropertiesForbidden() bool {
	var b bool
	return json.Unmarshal(s.AdditionalProperties, &b) == nil && !b
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
