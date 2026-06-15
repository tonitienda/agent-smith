package composition_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

// TestRenderEmpty shows a friendly note rather than an empty table when nothing
// occupies the window yet.
func TestRenderEmpty(t *testing.T) {
	proj := projection.Project(nil, projection.Options{})
	c := composition.Build(proj, cost.Embedded(), model, base, composition.SortSize)
	out := composition.Render(c)
	if !strings.Contains(out, "empty") {
		t.Errorf("empty composition render = %q, want an 'empty' note", out)
	}
}

// TestRenderSections checks the panel leads with the window total and top
// consumers and surfaces a duplicate read — the highlights the AC promises.
func TestRenderSections(t *testing.T) {
	events := []schema.Block{
		fileRead("r1", "dup.go", 4000, 2),
		fileRead("r2", "dup.go", 4000, 8),
		text("u", schema.RoleUser, 40, 1),
	}
	proj := projection.Project(events, projection.Options{TargetModel: model})
	c := composition.Build(proj, cost.Embedded(), model, base, composition.SortSize)
	out := composition.Render(c)

	for _, want := range []string{"Context composition", "Window:", "Top consumers", "By type", "Duplicate reads", "dup.go", "All segments"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n---\n%s", want, out)
		}
	}
	// Top consumers must appear before the full list, so the eye lands on them.
	if strings.Index(out, "Top consumers") > strings.Index(out, "All segments") {
		t.Error("Top consumers must render before the full segment list")
	}
}

// TestRenderUnknownModel notes the missing price instead of printing a confident
// zero-dollar figure.
func TestRenderUnknownModel(t *testing.T) {
	events := []schema.Block{textBlock("a", 40)}
	proj := projection.Project(events, projection.Options{})
	c := composition.Build(proj, cost.Embedded(), "no-such-model", base, composition.SortSize)
	out := composition.Render(c)
	if !strings.Contains(out, "no pricing entry") {
		t.Errorf("render = %q, want a no-pricing note", out)
	}
}

func textBlock(id string, chars int) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: schema.RoleUser,
		TS:   base.Add(-time.Minute),
		Text: &schema.TextBody{Text: strings.Repeat("x", chars)},
	}
}
