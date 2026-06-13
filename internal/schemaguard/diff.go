package schemaguard

import "fmt"

// Compare diffs a current schema descriptor against a committed baseline and
// returns a message for every breaking (non-additive) change, in deterministic
// order. An empty result means the change is additive-only and therefore
// allowed (PRD D2). Additions — new types, new fields, new enum values — are
// never reported; only removals, renames, type changes, wire-presence changes,
// and dropped enum values are.
//
// Field identity is the Go field name: a renamed field reads as a removal of
// the old name (and a silent, allowed addition of the new one), which is the
// correct verdict — the old wire contract is gone. A field that keeps its Go
// name but changes its json tag is reported as a rename of the wire key.
func Compare(baseline, current Descriptor) []string {
	var breaks []string

	for _, typeName := range sortedKeys(baseline.Types) {
		cur, ok := current.Types[typeName]
		if !ok {
			breaks = append(breaks, fmt.Sprintf("type %q was removed (types are permanent under additive-only)", typeName))
			continue
		}
		curFields := make(map[string]Field, len(cur.Fields))
		for _, f := range cur.Fields {
			curFields[f.Name] = f
		}
		for _, bf := range baseline.Types[typeName].Fields {
			cf, ok := curFields[bf.Name]
			if !ok {
				breaks = append(breaks, fmt.Sprintf("%s.%s (json %q) was removed or renamed", typeName, bf.Name, bf.JSON))
				continue
			}
			if cf.JSON != bf.JSON {
				breaks = append(breaks, fmt.Sprintf("%s.%s json key changed %q -> %q (wire renames forbidden)", typeName, bf.Name, bf.JSON, cf.JSON))
			}
			if cf.Type != bf.Type {
				breaks = append(breaks, fmt.Sprintf("%s.%s type changed %q -> %q (type changes forbidden)", typeName, bf.Name, bf.Type, cf.Type))
			}
			if cf.OmitEmpty != bf.OmitEmpty {
				breaks = append(breaks, fmt.Sprintf("%s.%s omitempty changed %v -> %v (wire-presence changes forbidden)", typeName, bf.Name, bf.OmitEmpty, cf.OmitEmpty))
			}
			if cf.AsString != bf.AsString {
				breaks = append(breaks, fmt.Sprintf("%s.%s json `,string` option changed %v -> %v (wire-encoding changes forbidden)", typeName, bf.Name, bf.AsString, cf.AsString))
			}
		}
	}

	for _, enumName := range sortedKeys(baseline.Enums) {
		cur, ok := current.Enums[enumName]
		if !ok {
			breaks = append(breaks, fmt.Sprintf("enum %q was removed (enums are permanent under additive-only)", enumName))
			continue
		}
		present := make(map[string]bool, len(cur))
		for _, v := range cur {
			present[v] = true
		}
		for _, v := range baseline.Enums[enumName] {
			if !present[v] {
				breaks = append(breaks, fmt.Sprintf("enum %s value %q was removed or renamed (enum values are permanent)", enumName, v))
			}
		}
	}

	return breaks
}
