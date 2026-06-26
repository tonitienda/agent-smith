// Package initenrich is the provider-backed implementation of the AS-087 /init
// model-assisted enrichment pass (PRD §7.16). It turns the deterministic scan
// facts (initscaffold.Facts) into a few grounded prose sections — what the
// project is, its conventions, its gotchas — by asking a cheap-tier model to
// read the facts and draft them.
//
// It lives outside internal/initscaffold on purpose: the scaffold points
// strictly at the filesystem and stays deterministic and offline, so the part
// that reaches a provider and the routing policy lives here and is wired in by
// the composition root (cmd/smith). The package implements initscaffold.Enricher,
// so /init calls it through that seam without importing provider or routing.
//
// Budget posture: enrichment is opt-in (/init --describe), runs on the cheap
// routing tier, and bounds the reply with maxOutputTokens — a cheap model with a
// short reply is a fraction of a cent. It never replaces the deterministic
// build/test/lint section; initscaffold only ever appends what this returns.
package initenrich

import (
	"context"
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/initscaffold"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/schema"
)

// maxOutputTokens bounds the model's reply. A handful of short prose sections
// fits comfortably; the cap keeps the cheap-tier call's cost negligible.
const maxOutputTokens = 700

// maxSections caps how many prose sections one pass appends, so /init stays a
// short, reviewable memory file rather than a wall of generated text.
const maxSections = 3

// Enricher drafts memory-file prose with a cheap-tier model call. Build it with
// New and pass it to the /init command; it satisfies initscaffold.Enricher.
type Enricher struct {
	p         provider.Provider
	router    routing.Policy
	baseModel string
}

// New builds an Enricher over the session's provider and routing policy.
// baseModel is the session's active model (the routing fallback when the policy
// maps no cheap tier for the vendor).
func New(p provider.Provider, router routing.Policy, baseModel string) *Enricher {
	return &Enricher{p: p, router: router, baseModel: baseModel}
}

// compile-time check that *Enricher satisfies the seam /init calls.
var _ initscaffold.Enricher = (*Enricher)(nil)

// Enrich asks the cheap-tier model for grounded prose sections over the scan
// facts. A nil/unconfigured enricher yields nothing; an empty or garbled reply
// yields no sections (never an error) so /init degrades to the deterministic
// scaffold. It surfaces an error only when the turn itself fails to run.
func (e *Enricher) Enrich(ctx context.Context, f initscaffold.Facts) ([]initscaffold.ProseSection, error) {
	if e == nil || e.p == nil {
		return nil, nil
	}
	model := e.router.Resolve(routing.Cheap, e.p.Name(), e.baseModel)

	req := provider.Request{
		Model: model,
		Context: []schema.Block{
			systemBlock(systemPrompt),
			userBlock(renderFacts(f)),
		},
		Params: provider.SamplingParams{MaxTokens: maxOutputTokens},
		Cache:  provider.CacheHints{Disabled: true}, // a one-shot prefix that will never recur
	}

	stream, err := e.p.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("init enrichment pass: %w", err)
	}
	events, err := provider.Collect(stream)
	if err != nil {
		return nil, fmt.Errorf("init enrichment pass: %w", err)
	}

	var text strings.Builder
	for _, ev := range events {
		if ev.Type == provider.EventTextDelta {
			text.WriteString(ev.TextDelta)
		}
	}
	return parse(text.String()), nil
}

// systemPrompt instructs the cheap model to act as the /init enrichment layer:
// describe what the project is and how to work in it, grounded only in the facts
// it is given, and never restate the deterministic commands or layout.
const systemPrompt = `You are Agent Smith's /init enrichment layer. You are given deterministic facts about a software project — its name, its build/test/lint commands, its source layout, and a README excerpt — and you draft a few short prose sections for the project's AGENT.md memory file: what the project IS and how to work in it, the things a newcomer agent cannot derive mechanically.

Rules:
- Write between 1 and 3 sections. Each is a level-2 Markdown heading on its own line ("## Title") followed by a few sentences. Good titles: "Overview", "Conventions", "Gotchas".
- Ground every statement in the facts or README you were given. Never invent build/test/lint commands, file paths, dependencies, or behavior you were not told about.
- Do NOT restate the build/test/lint commands or the source-directory list — those are already documented deterministically. Add only what they do not cover.
- If the facts are too thin to say anything grounded and specific, reply with nothing at all.
- Reply with ONLY the Markdown sections: no preamble, no closing remarks, no code fences.`

// renderFacts formats the deterministic scan facts as the user message. It hands
// the model the commands and layout for context but tells it they are already
// documented, so the prose adds rather than repeats.
func renderFacts(f initscaffold.Facts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project name: %s\n", f.ProjectName)
	if f.Build != "" || f.Test != "" || f.Lint != "" {
		b.WriteString("\nCommands (already documented deterministically — do not repeat them):\n")
		if f.Build != "" {
			fmt.Fprintf(&b, "- build: %s\n", f.Build)
		}
		if f.Test != "" {
			fmt.Fprintf(&b, "- test: %s\n", f.Test)
		}
		if f.Lint != "" {
			fmt.Fprintf(&b, "- lint: %s\n", f.Lint)
		}
	}
	if len(f.Layout) > 0 {
		fmt.Fprintf(&b, "\nSource directories (already documented): %s\n", strings.Join(f.Layout, ", "))
	}
	if f.Readme != "" {
		b.WriteString("\nREADME excerpt:\n")
		b.WriteString(f.Readme)
		if !strings.HasSuffix(f.Readme, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// parse extracts the level-2 Markdown sections from the model's reply, tolerating
// a code fence around the whole thing. Text before the first "## " heading (a
// stray preamble or a top-level title) is ignored. A reply with no usable section
// yields nothing — the deterministic scaffold stands on its own.
func parse(reply string) []initscaffold.ProseSection {
	reply = stripFences(reply)

	var out []initscaffold.ProseSection
	var title string
	var body strings.Builder
	flush := func() {
		if title != "" && strings.TrimSpace(body.String()) != "" {
			out = append(out, initscaffold.ProseSection{Title: title, Body: strings.TrimSpace(body.String())})
		}
		title = ""
		body.Reset()
	}
	for _, line := range strings.Split(reply, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			title = strings.TrimSpace(h)
			continue
		}
		if title != "" {
			body.WriteString(line + "\n")
		}
	}
	flush()

	if len(out) > maxSections {
		out = out[:maxSections]
	}
	return out
}

// stripFences removes a single Markdown code fence wrapping the whole reply,
// which a model sometimes adds despite being told not to.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		return ""
	}
	return strings.TrimSuffix(strings.TrimRight(s, " \n"), "```")
}

// systemBlock / userBlock build the two model-facing context blocks the pass
// sends: a system instruction and the rendered scan facts as user text.
func systemBlock(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: text}}
}

func userBlock(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: text}}
}
