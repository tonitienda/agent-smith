package initenrich

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/initscaffold"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
)

// textEvents scripts a single text block streamed as one delta.
func textEvents(s string) []provider.Event {
	return []provider.Event{{Type: provider.EventTextDelta, TextDelta: s}}
}

// AS-087: Enrich resolves the cheap routing tier for the vendor and parses the
// model's level-2 sections into ProseSections.
func TestEnrichParsesSectionsAndUsesCheapTier(t *testing.T) {
	reply := "## Overview\nX is a widget service.\n\n## Conventions\nKeep handlers thin.\n"
	p := &provider.Mock{NameValue: "anthropic", Events: textEvents(reply)}
	e := New(p, routing.Default(), "claude-opus-4-8")

	secs, err := e.Enrich(context.Background(), initscaffold.Facts{ProjectName: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 || secs[0].Title != "Overview" || secs[1].Title != "Conventions" {
		t.Fatalf("unexpected sections: %+v", secs)
	}
	if !strings.Contains(secs[0].Body, "widget service") {
		t.Errorf("body lost: %q", secs[0].Body)
	}
	// Cheap tier resolves haiku for anthropic in the default policy.
	if got := p.Requests()[0].Model; got != "claude-haiku-4-5" {
		t.Errorf("model = %q, want cheap-tier claude-haiku-4-5", got)
	}
	if p.Requests()[0].Params.MaxTokens != maxOutputTokens {
		t.Errorf("MaxTokens = %d, want bounded %d", p.Requests()[0].Params.MaxTokens, maxOutputTokens)
	}
}

// A stream failure surfaces as an error so /init can fall back.
func TestEnrichStreamErrorSurfaces(t *testing.T) {
	p := &provider.Mock{OpenErr: errors.New("boom")}
	if _, err := New(p, routing.Default(), "m").Enrich(context.Background(), initscaffold.Facts{}); err == nil {
		t.Fatal("want error, got nil")
	}
}

// A garbled reply (no headings) yields no sections, never an error — the
// deterministic scaffold stands on its own.
func TestEnrichGarbledReplyYieldsNothing(t *testing.T) {
	p := &provider.Mock{Events: textEvents("sorry, I cannot help with that")}
	secs, err := New(p, routing.Default(), "m").Enrich(context.Background(), initscaffold.Facts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 0 {
		t.Errorf("want no sections, got %+v", secs)
	}
}

// A nil/unconfigured enricher is a no-op.
func TestEnrichNil(t *testing.T) {
	var e *Enricher
	secs, err := e.Enrich(context.Background(), initscaffold.Facts{})
	if err != nil || secs != nil {
		t.Errorf("nil enricher: secs=%v err=%v", secs, err)
	}
}

func TestParseStripsCodeFenceAndCapsSections(t *testing.T) {
	reply := "```markdown\n## A\nbody a\n## B\nbody b\n## C\nbody c\n## D\nbody d\n```"
	secs := parse(reply)
	if len(secs) != maxSections {
		t.Fatalf("want %d sections (capped), got %d: %+v", maxSections, len(secs), secs)
	}
	if secs[0].Title != "A" || secs[0].Body != "body a" {
		t.Errorf("fence not stripped / wrong parse: %+v", secs[0])
	}
}

// Preamble before the first heading is ignored rather than swallowing the first
// section's title.
func TestParseIgnoresPreamble(t *testing.T) {
	secs := parse("Here are the sections:\n\n## Overview\nthe body\n")
	if len(secs) != 1 || secs[0].Title != "Overview" || secs[0].Body != "the body" {
		t.Fatalf("unexpected parse: %+v", secs)
	}
}

// renderFacts tells the model the commands are already documented so the prose
// adds rather than repeats, and includes the README sample.
func TestRenderFactsGroundsAndDeduplicates(t *testing.T) {
	out := renderFacts(initscaffold.Facts{
		ProjectName: "x",
		Test:        "go test ./...",
		Layout:      []string{"cmd", "internal"},
		Readme:      "X is a widget service.",
	})
	for _, want := range []string{"x", "go test ./...", "already documented", "cmd, internal", "widget service"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderFacts missing %q:\n%s", want, out)
		}
	}
}
