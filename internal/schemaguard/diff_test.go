package schemaguard

import (
	"strings"
	"testing"
)

// baseFixture is a tiny two-type, one-enum descriptor used to exercise Compare
// in isolation from the real schema.
func baseFixture() Descriptor {
	return Descriptor{
		Types: map[string]Type{
			"Block": {Fields: []Field{
				{Name: "ID", JSON: "id", Type: "string", OmitEmpty: false},
				{Name: "Tokens", JSON: "tokens", Type: "*Tokens", OmitEmpty: true},
			}},
			"Tokens": {Fields: []Field{
				{Name: "Input", JSON: "input", Type: "*int", OmitEmpty: true},
			}},
		},
		Enums: map[string][]string{
			"Kind": {"text", "tool_call"},
		},
	}
}

func TestCompareAllowsAdditions(t *testing.T) {
	base := baseFixture()
	cur := baseFixture()
	// Add a new field, a new type, and a new enum value — all additive.
	b := cur.Types["Block"]
	b.Fields = append(b.Fields, Field{Name: "Cost", JSON: "cost_usd", Type: "*float64", OmitEmpty: true})
	cur.Types["Block"] = b
	cur.Types["NewBody"] = Type{Fields: []Field{{Name: "X", JSON: "x", Type: "string", OmitEmpty: true}}}
	cur.Enums["Kind"] = append(cur.Enums["Kind"], "reasoning")
	cur.Enums["Role"] = []string{"user"}

	if breaks := Compare(base, cur); len(breaks) != 0 {
		t.Fatalf("additions must be allowed, got breaks: %v", breaks)
	}
}

func TestCompareDetectsBreakingChanges(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Descriptor)
		wantSub string
	}{
		{
			name:    "removed field",
			mutate:  func(d *Descriptor) { d.Types["Block"] = Type{Fields: d.Types["Block"].Fields[:1]} },
			wantSub: "Block.Tokens",
		},
		{
			name: "renamed json key",
			mutate: func(d *Descriptor) {
				f := d.Types["Block"].Fields
				f[0].JSON = "block_id"
				d.Types["Block"] = Type{Fields: f}
			},
			wantSub: "json key changed",
		},
		{
			name: "type change",
			mutate: func(d *Descriptor) {
				f := d.Types["Tokens"].Fields
				f[0].Type = "int"
				d.Types["Tokens"] = Type{Fields: f}
			},
			wantSub: "type changed",
		},
		{
			name: "omitempty change",
			mutate: func(d *Descriptor) {
				f := d.Types["Block"].Fields
				f[0].OmitEmpty = true
				d.Types["Block"] = Type{Fields: f}
			},
			wantSub: "omitempty changed",
		},
		{
			name: "string option change",
			mutate: func(d *Descriptor) {
				f := d.Types["Block"].Fields
				f[0].AsString = true
				d.Types["Block"] = Type{Fields: f}
			},
			wantSub: "`,string` option changed",
		},
		{
			name:    "removed type",
			mutate:  func(d *Descriptor) { delete(d.Types, "Tokens") },
			wantSub: `type "Tokens" was removed`,
		},
		{
			name:    "dropped enum value",
			mutate:  func(d *Descriptor) { d.Enums["Kind"] = []string{"text"} },
			wantSub: `enum Kind value "tool_call"`,
		},
		{
			name:    "removed enum",
			mutate:  func(d *Descriptor) { delete(d.Enums, "Kind") },
			wantSub: `enum "Kind" was removed`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cur := baseFixture()
			tt.mutate(&cur)
			breaks := Compare(baseFixture(), cur)
			if len(breaks) == 0 {
				t.Fatalf("expected a breaking change, got none")
			}
			joined := strings.Join(breaks, "\n")
			if !strings.Contains(joined, tt.wantSub) {
				t.Fatalf("break message missing %q, got:\n%s", tt.wantSub, joined)
			}
		})
	}
}
