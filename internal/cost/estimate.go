package cost

import (
	"unicode/utf8"

	"github.com/tonitienda/agent-smith/schema"
)

// Per-block token estimation (AS-063). AS-020 prices whole turns from the usage
// counts a provider reports, which reconciles exactly with the bill. What it
// cannot give is the share of the window any *single* block occupies — the data
// the always-visible context meter (AS-025) and the /context composition view
// (AS-026) need to say "this block is N tokens of your window". A real tokenizer
// is per-vendor and would pull a heavy dependency, against the repo's
// stdlib-only default (PRD D6 scope discipline), so this is a deliberate
// heuristic.
//
// Method: ~charsPerToken characters per token, the well-known rule of thumb for
// English text (OpenAI/Anthropic both document roughly four characters or
// three-quarters of a word per token). Counted in runes, not bytes, so a
// multi-byte character is one character, not several.
//
// Accuracy: within ~10-20% for ordinary English prose. It runs higher (more
// real tokens per character) for dense JSON, code, and non-Latin scripts, where
// the tokenizer splits more finely; it is therefore an estimate for composition
// display, never a billing figure — billing always uses the provider-reported
// counts in AS-020. The estimate is deterministic and model-agnostic, matching
// the projection engine's contract (AS-006).
const charsPerToken = 4

// EstimateTokens returns a heuristic token estimate for s using the
// chars-per-token approximation (see the package note above). It is the single
// estimation primitive the per-block and per-window helpers build on.
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	// Round up so any non-empty content counts as at least one token.
	return (n + charsPerToken - 1) / charsPerToken
}

// EstimateBlockTokens estimates the tokens one block contributes to the window
// by summing the model-facing textual payload it carries and applying the
// chars-per-token heuristic. Control events (usage, exclusion, model-switch)
// carry no such payload and so estimate to zero, which is what lets
// EstimateContextTokens sum a raw log slice without filtering them out. It counts
// runes per field directly rather than concatenating them, so estimating a large
// file-read or tool-result block allocates nothing.
func EstimateBlockTokens(b schema.Block) int {
	n := blockRuneCount(b)
	if n == 0 {
		return 0
	}
	// Round up so any non-empty content counts as at least one token.
	return (n + charsPerToken - 1) / charsPerToken
}

// EstimateContextTokens sums the per-block estimates over events — the
// window-occupancy figure /context (AS-026) attributes block by block. Pass a
// projection's live blocks (projection.Projection.Live()) for the model-facing
// window, or a full log slice; control events contribute nothing either way.
func EstimateContextTokens(events []schema.Block) int {
	total := 0
	for i := range events {
		total += EstimateBlockTokens(events[i])
	}
	return total
}

// blockRuneCount counts the runes of the textual payload a block presents to the
// model, across whichever body matches its Kind, without allocating an
// intermediate string. It is intentionally inclusive — tool-call arguments and
// tool-result stdout/stderr are part of the window the model pays for, so they
// count toward the block's size.
func blockRuneCount(b schema.Block) int {
	var count int
	switch {
	case b.Text != nil:
		count += utf8.RuneCountInString(b.Text.Text)
		count += partsRuneCount(b.Text.Parts)
	case b.ToolCall != nil:
		count += utf8.RuneCountInString(b.ToolCall.Name)
		if len(b.ToolCall.Arguments) > 0 {
			count += utf8.RuneCount(b.ToolCall.Arguments)
		} else {
			count += utf8.RuneCountInString(b.ToolCall.ArgumentsRaw)
		}
	case b.ToolResult != nil:
		count += utf8.RuneCountInString(b.ToolResult.Stdout)
		count += utf8.RuneCountInString(b.ToolResult.Stderr)
		count += utf8.RuneCount(b.ToolResult.StructuredContent)
		count += partsRuneCount(b.ToolResult.Content)
	case b.FileRead != nil:
		count += utf8.RuneCountInString(b.FileRead.Content)
	case b.Reasoning != nil:
		count += utf8.RuneCountInString(b.Reasoning.Text)
		for _, s := range b.Reasoning.Summary {
			count += utf8.RuneCountInString(s)
		}
	}
	return count
}

// partsRuneCount returns the rune count of the text parts in a multimodal slice.
// Binary parts (image/audio/file) carry no estimable text — their token cost is
// vendor-specific and out of scope for this heuristic — so only Text is summed.
func partsRuneCount(parts []schema.Part) int {
	var count int
	for _, p := range parts {
		count += utf8.RuneCountInString(p.Text)
	}
	return count
}
