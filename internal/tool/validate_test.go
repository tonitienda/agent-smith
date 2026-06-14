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
		{name: "union type property not checked", schema: `{"type":"object","properties":{"x":{"type":["string","number"]}}}`, args: `{"x":true}`},
		{name: "additionalProperties allowed by default", schema: `{"type":"object","properties":{"a":{"type":"string"}}}`, args: `{"a":"x","b":2}`},
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
