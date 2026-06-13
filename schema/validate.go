package schema

import "fmt"

// Validate checks a block's structural invariants: a stable ID, a kind, and —
// for the five frozen V1 content kinds — exactly the matching body set and no
// other content body. Unknown/future kinds (e.g. derived kinds, or kinds a
// newer producer added) are tolerated and impose no body constraint, honoring
// the additive-only, tolerate-unknown discipline (PRD D2).
//
// Validate is intentionally lenient about optional metadata: absent provenance,
// tokens, provider, etc. are all valid (they are filled later, or simply not
// reported by the source surface).
func (b Block) Validate() error {
	if b.ID == "" {
		return fmt.Errorf("schema: block has no id")
	}
	if b.Kind == "" {
		return fmt.Errorf("schema: block %s has no kind", b.ID)
	}

	set := b.setBodies()
	if !b.Kind.IsContentKind() {
		// Forward-compat: a kind we do not recognize may legitimately carry a
		// body shape we do not model. Do not reject it.
		return nil
	}
	if len(set) != 1 || set[0] != b.Kind {
		return fmt.Errorf("schema: block %s of kind %q must set exactly its matching body, got bodies %v", b.ID, b.Kind, set)
	}
	return nil
}

// setBodies returns the kinds whose body pointers are non-nil on b.
func (b Block) setBodies() []Kind {
	var set []Kind
	if b.Text != nil {
		set = append(set, KindText)
	}
	if b.ToolCall != nil {
		set = append(set, KindToolCall)
	}
	if b.ToolResult != nil {
		set = append(set, KindToolResult)
	}
	if b.FileRead != nil {
		set = append(set, KindFileRead)
	}
	if b.Reasoning != nil {
		set = append(set, KindReasoning)
	}
	return set
}
