package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateArgs(t *testing.T) {
	const objSchema = `{
		"type": "object",
		"required": ["path"],
		"additionalProperties": false,
		"properties": {
			"path":   {"type": "string"},
			"limit":  {"type": "integer"},
			"ratio":  {"type": "number"},
			"deep":   {"type": "boolean"}
		}
	}`

	cases := []struct {
		name    string
		schema  string
		args    string
		wantErr string // substring; "" means accept
	}{
		{name: "valid", schema: objSchema, args: `{"path":"a.go","limit":10}`},
		{name: "valid integer satisfies number", schema: objSchema, args: `{"path":"a.go","ratio":3}`},
		{name: "valid float ratio", schema: objSchema, args: `{"path":"a.go","ratio":3.5}`},
		{name: "missing required", schema: objSchema, args: `{"limit":10}`, wantErr: `required property "path"`},
		{name: "wrong scalar type", schema: objSchema, args: `{"path":123}`, wantErr: `property "path" must be string`},
		{name: "float for integer", schema: objSchema, args: `{"path":"a","limit":1.5}`, wantErr: `property "limit" must be integer`},
		{name: "unexpected property", schema: objSchema, args: `{"path":"a","nope":1}`, wantErr: `unexpected property "nope"`},
		{name: "non-object args", schema: objSchema, args: `[1,2,3]`, wantErr: "must be a JSON object"},
		{name: "empty args treated as empty object", schema: `{"type":"object"}`, args: ``},
		{name: "null args treated as empty object", schema: `{"type":"object"}`, args: `null`},
		{name: "no schema accepts anything", schema: ``, args: `{"whatever":true}`},
		{name: "malformed schema is lenient", schema: `{not json`, args: `{"path":1}`},
		{name: "non-object schema skipped", schema: `{"type":"string"}`, args: `"hello"`},
		{name: "union type accepts a member", schema: `{"type":"object","properties":{"x":{"type":["string","number"]}}}`, args: `{"x":3}`},
		{name: "union type rejects a non-member", schema: `{"type":"object","properties":{"x":{"type":["string","number"]}}}`, args: `{"x":true}`, wantErr: `property "x" must be string or number, got boolean`},
		{name: "additionalProperties allowed by default", schema: `{"type":"object","properties":{"a":{"type":"string"}}}`, args: `{"a":"x","b":2}`},

		// AS-062: enum.
		{name: "enum accepts a member", schema: `{"type":"object","properties":{"color":{"enum":["red","green"]}}}`, args: `{"color":"green"}`},
		{name: "enum rejects a non-member", schema: `{"type":"object","properties":{"color":{"enum":["red","green"]}}}`, args: `{"color":"blue"}`, wantErr: `property "color" must be one of "red", "green"`},
		{name: "enum matches number regardless of form", schema: `{"type":"object","properties":{"n":{"enum":[1,2]}}}`, args: `{"n":2.0}`},

		// AS-062: numeric bounds.
		{name: "minimum satisfied", schema: `{"type":"object","properties":{"n":{"type":"integer","minimum":1}}}`, args: `{"n":1}`},
		{name: "below minimum", schema: `{"type":"object","properties":{"n":{"type":"integer","minimum":1}}}`, args: `{"n":0}`, wantErr: `property "n" must be >= 1`},
		{name: "above maximum", schema: `{"type":"object","properties":{"r":{"type":"number","maximum":1.5}}}`, args: `{"r":2}`, wantErr: `property "r" must be <= 1.5`},

		// AS-062: string length and pattern.
		{name: "minLength satisfied", schema: `{"type":"object","properties":{"s":{"type":"string","minLength":2}}}`, args: `{"s":"ab"}`},
		{name: "below minLength", schema: `{"type":"object","properties":{"s":{"type":"string","minLength":2}}}`, args: `{"s":"a"}`, wantErr: `property "s" must be at least 2 characters`},
		{name: "above maxLength", schema: `{"type":"object","properties":{"s":{"type":"string","maxLength":3}}}`, args: `{"s":"abcd"}`, wantErr: `property "s" must be at most 3 characters`},
		{name: "minLength counts code points", schema: `{"type":"object","properties":{"s":{"type":"string","minLength":2}}}`, args: `{"s":"é€"}`},
		{name: "pattern matches", schema: `{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-z]+$"}}}`, args: `{"id":"abc"}`},
		{name: "pattern mismatch", schema: `{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-z]+$"}}}`, args: `{"id":"AB1"}`, wantErr: `property "id" must match pattern "^[a-z]+$"`},
		{name: "uncompilable pattern is lenient", schema: `{"type":"object","properties":{"id":{"type":"string","pattern":"("}}}`, args: `{"id":"x"}`},

		// AS-062: array items and bounds.
		{name: "minItems satisfied", schema: `{"type":"object","properties":{"xs":{"type":"array","minItems":1}}}`, args: `{"xs":[1]}`},
		{name: "below minItems", schema: `{"type":"object","properties":{"xs":{"type":"array","minItems":1}}}`, args: `{"xs":[]}`, wantErr: `property "xs" must have at least 1 items, got 0`},
		{name: "above maxItems", schema: `{"type":"object","properties":{"xs":{"type":"array","maxItems":2}}}`, args: `{"xs":[1,2,3]}`, wantErr: `property "xs" must have at most 2 items, got 3`},
		{name: "array item type checked", schema: `{"type":"object","properties":{"xs":{"type":"array","items":{"type":"string"}}}}`, args: `{"xs":["a",2]}`, wantErr: `property "xs[1]" must be string, got integer`},
		{name: "array item enum checked", schema: `{"type":"object","properties":{"xs":{"type":"array","items":{"enum":["a","b"]}}}}`, args: `{"xs":["a","z"]}`, wantErr: `property "xs[1]" must be one of "a", "b"`},
		{name: "tuple-form items skipped leniently", schema: `{"type":"object","properties":{"xs":{"type":"array","items":[{"type":"string"}]}}}`, args: `{"xs":[1,2]}`},

		// AS-062: nested objects, named by full path.
		{name: "nested required missing", schema: `{"type":"object","properties":{"filter":{"type":"object","required":["kind"]}}}`, args: `{"filter":{}}`, wantErr: `missing required property "filter.kind"`},
		{name: "nested type mismatch", schema: `{"type":"object","properties":{"filter":{"type":"object","properties":{"kind":{"type":"string"}}}}}`, args: `{"filter":{"kind":7}}`, wantErr: `property "filter.kind" must be string, got integer`},
		{name: "nested unexpected property", schema: `{"type":"object","properties":{"filter":{"type":"object","additionalProperties":false,"properties":{"kind":{"type":"string"}}}}}`, args: `{"filter":{"kind":"x","extra":1}}`, wantErr: `unexpected property "filter.extra"`},

		// AS-062: unmodeled keywords still never falsely reject.
		{name: "unknown keyword ignored", schema: `{"type":"object","properties":{"x":{"type":"string","format":"email","oneOf":[{"const":"a"}]}}}`, args: `{"x":"anything"}`},
		{name: "boolean subschema ignored", schema: `{"type":"object","properties":{"x":true}}`, args: `{"x":{"deep":1}}`},
		{name: "declared boolean property not unexpected under additionalProperties false", schema: `{"type":"object","additionalProperties":false,"properties":{"x":true}}`, args: `{"x":42}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArgs(json.RawMessage(tc.schema), json.RawMessage(tc.args))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("validateArgs() = %v, want nil", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("validateArgs() = nil, want error containing %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("validateArgs() = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestJSONType(t *testing.T) {
	cases := map[string]string{
		`"s"`:     "string",
		`42`:      "integer",
		`-7`:      "integer",
		`3.14`:    "number",
		`1e3`:     "number",
		`true`:    "boolean",
		`false`:   "boolean",
		`null`:    "null",
		`{"a":1}`: "object",
		`[1,2]`:   "array",
		`  "x"  `: "string",
	}
	for in, want := range cases {
		if got := jsonType(json.RawMessage(in)); got != want {
			t.Errorf("jsonType(%s) = %q, want %q", in, got, want)
		}
	}
}
