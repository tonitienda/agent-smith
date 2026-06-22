package clean_test

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/clean"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/schema"
)

// msg is a text block carrying real words (the matcher reads content, unlike the
// char-count helpers the AS-028 tests use).
func msg(id string, role schema.Role, body string, ageMinutes int) schema.Block {
	b := text(id, role, 0, ageMinutes)
	b.Role = role
	b.Text = &schema.TextBody{Text: body}
	return b
}

// fileRead is a file-read block at path with a little content, so its module
// tag (AS-027) is matchable while its body is deliberately not.
func fileRead(id, path, content string, ageMinutes int) schema.Block {
	b := text(id, schema.RoleTool, 0, ageMinutes)
	b.Kind = schema.KindFileRead
	b.Text = nil
	b.FileRead = &schema.FileReadBody{Path: path, Content: content}
	return b
}

func matchIDs(events []schema.Block, query string) []string {
	ids, _ := clean.Match(project(events), query)
	return ids
}

func has(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// TestMatchDemoScenario is the headline AC (PRD §7.12): fix bug A, move to task
// B, then `/clean "the bug we fixed"` reclaims A's segments and leaves B's.
func TestMatchDemoScenario(t *testing.T) {
	events := []schema.Block{
		msg("blk_bugA1", schema.RoleUser, "there is a bug in the auth token refresh", 30),
		msg("blk_bugA2", schema.RoleAssistant, "I fixed the bug by refreshing the auth token", 29),
		msg("blk_taskB1", schema.RoleUser, "now add a CSV export to the report page", 10),
		msg("blk_taskB2", schema.RoleAssistant, "added the CSV export and a download button", 9),
	}
	ids := matchIDs(events, "the content related to the bug we fixed")
	if !has(ids, "blk_bugA1") || !has(ids, "blk_bugA2") {
		t.Fatalf("want bug-A segments selected, got %v", ids)
	}
	if has(ids, "blk_taskB1") || has(ids, "blk_taskB2") {
		t.Errorf("task-B segments must not be selected, got %v", ids)
	}
}

// TestMatchExplains covers the AC that the preview explains *why* each segment
// matched (the terms it hit), so the user can trust or correct the selection.
func TestMatchExplains(t *testing.T) {
	events := []schema.Block{
		msg("blk_x1", schema.RoleAssistant, "fixed the auth bug", 20),
	}
	ids, why := clean.Match(project(events), "auth bug")
	if len(ids) != 1 {
		t.Fatalf("ids = %v, want one match", ids)
	}
	reason := why["blk_x1"]
	if !strings.Contains(reason, "auth") || !strings.Contains(reason, "bug") {
		t.Errorf("explanation %q should name the matched terms auth, bug", reason)
	}
}

// TestMatchRanksMoreSpecificFirst: a segment hitting more distinct query terms
// ranks ahead of one hitting fewer (more specific first, AC explainability).
func TestMatchRanksMoreSpecificFirst(t *testing.T) {
	events := []schema.Block{
		msg("blk_one", schema.RoleAssistant, "the auth code", 20),
		msg("blk_two", schema.RoleAssistant, "the auth token bug we fixed", 19),
	}
	ids := matchIDs(events, "auth token bug")
	if len(ids) < 2 || ids[0] != "blk_two" {
		t.Fatalf("want blk_two (3 terms) ranked first, got %v", ids)
	}
}

// TestMatchByFileModuleAndTool covers matching against AS-027 tags: a file's
// module path and a tool's name, not just free text.
func TestMatchByFileModuleAndTool(t *testing.T) {
	events := []schema.Block{
		fileRead("blk_f1", "internal/payments/charge.go", "package payments", 20),
		toolCall("blk_t1", "use1", "grep", 12, 19),
		msg("blk_m1", schema.RoleUser, "unrelated chatter about lunch", 18),
	}
	if ids := matchIDs(events, "payments"); !has(ids, "blk_f1") || has(ids, "blk_m1") {
		t.Errorf("payments should match the payments file only, got %v", ids)
	}
	if ids := matchIDs(events, "grep"); !has(ids, "blk_t1") {
		t.Errorf("grep should match the grep tool call, got %v", ids)
	}
}

// TestMatchAllStopwordsSelectsNothing: a query with no significant terms must
// match nothing rather than everything (conservative under-selection, AC).
func TestMatchAllStopwordsSelectsNothing(t *testing.T) {
	events := []schema.Block{
		msg("blk_a", schema.RoleUser, "anything at all here", 10),
	}
	for _, q := range []string{"the content we", "   ", ""} {
		if ids := matchIDs(events, q); len(ids) != 0 {
			t.Errorf("query %q matched %v, want nothing", q, ids)
		}
	}
}

// TestMatchSkipsExcluded: a block already removed from the window is not a
// match candidate — /clean operates on the live projection only.
func TestMatchSkipsExcluded(t *testing.T) {
	events := []schema.Block{
		msg("blk_live", schema.RoleAssistant, "the parser bug is fixed", 20),
		msg("blk_gone", schema.RoleAssistant, "an earlier parser note", 25),
	}
	// Remove blk_gone first, then match: it must not come back as a candidate.
	p := clean.Preview(project(events), cost.Embedded(), model, base, []string{"blk_gone"})
	ev, ok := clean.Apply(p)
	if !ok {
		t.Fatal("expected an exclusion to apply")
	}
	events = append(events, ev)
	if ids := matchIDs(events, "parser"); has(ids, "blk_gone") {
		t.Errorf("excluded block must not match, got %v", ids)
	}
}

// TestPreviewMatchPairsAndPrices: the topic path reuses Preview, so it inherits
// atomic tool-call/result pairing and the same token/$ accounting, and annotates
// the directly matched item with why it matched.
func TestPreviewMatchPairsAndPrices(t *testing.T) {
	events := []schema.Block{
		toolCall("blk_call", "use1", "shell", 40, 20),
		toolResult("blk_res", "use1", 80, 19),
	}
	// The call's args/tool name carry "shell"; the result is pulled in atomically.
	p := clean.PreviewMatch(project(events), cost.Embedded(), model, base, "shell")
	if len(p.Items) != 2 {
		t.Fatalf("want call+result removed atomically, got %d items: %+v", len(p.Items), p.Items)
	}
	var matched, paired clean.Item
	for _, it := range p.Items {
		if it.ID == "blk_call" {
			matched = it
		}
		if it.ID == "blk_res" {
			paired = it
		}
	}
	if matched.Why == "" {
		t.Errorf("matched item should carry a why-explanation, got %+v", matched)
	}
	if !paired.Paired || paired.Why != "" {
		t.Errorf("result should be flagged paired with no why, got %+v", paired)
	}
	if p.Tokens <= 0 {
		t.Errorf("expected non-zero reclaimed tokens, got %d", p.Tokens)
	}
}

// TestPreviewMatchNoMatchSurfacesQuery: when nothing matches, the preview is
// empty and names the query so the user knows it found nothing (not a silent
// no-op), and nothing is staged.
func TestPreviewMatchNoMatchSurfacesQuery(t *testing.T) {
	events := []schema.Block{
		msg("blk_a", schema.RoleUser, "hello world", 10),
	}
	p := clean.PreviewMatch(project(events), cost.Embedded(), model, base, "nonexistent topic")
	if !p.Empty() {
		t.Fatalf("expected an empty plan, got %+v", p.Items)
	}
	if len(p.Unknown) == 0 {
		t.Errorf("expected the query surfaced in Unknown, got %+v", p.Unknown)
	}
}
