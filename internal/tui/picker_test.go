package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/schema"
)

// modelWithRehydrate builds a sized model whose ResetView/launch transcript is
// rebuilt from the given blocks (AS-064 rehydration).
func modelWithRehydrate(t *testing.T, blocks []schema.Block) model {
	t.Helper()
	m := newModel(&fakeRunner{}, staticMeta(Meta{Model: "m"}),
		make(chan loop.UIEvent), nil, nil, nil, false, func() []schema.Block { return blocks }, nil, nil)
	return update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

func userText(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: text}}
}

func assistantText(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: text}}
}

// TestSegmentsFromBlocks checks the projected-block→segment converter rebuilds
// the conversation roles and pairs a tool call with its result the way the live
// loop does (AS-064 AC2).
func TestSegmentsFromBlocks(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "main.go"})
	blocks := []schema.Block{
		userText("read the file"),
		{Kind: schema.KindReasoning, Role: schema.RoleAssistant, Reasoning: &schema.ReasoningBody{Text: "planning"}},
		{Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{ToolUseID: "t1", Name: "read", Arguments: args}},
		{Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{ToolUseID: "t1", Content: []schema.Part{{Type: "text", Text: "package main"}}}},
		assistantText("here it is"),
	}
	// The tool_result folds into its call card, so 5 blocks → 4 segments.
	segs := segmentsFromBlocks(blocks)
	if len(segs) != 4 {
		t.Fatalf("got %d segments, want 4: %+v", len(segs), segs)
	}
	if segs[0].kind != segUser || segs[0].text != "read the file" {
		t.Errorf("seg0 = %+v, want user 'read the file'", segs[0])
	}
	if segs[1].kind != segReasoning {
		t.Errorf("seg1 kind = %v, want reasoning", segs[1].kind)
	}
	tool := segs[2]
	if tool.kind != segTool || tool.toolName != "read" || !tool.toolDone || !tool.toolSettled {
		t.Errorf("seg2 = %+v, want a settled 'read' tool card", tool)
	}
	if !strings.Contains(tool.toolResult, "package main") {
		t.Errorf("tool result = %q, want it to carry the result text", tool.toolResult)
	}
	if segs[3].kind != segAssistant || !segs[3].done {
		t.Errorf("seg3 = %+v, want a done assistant span", segs[3])
	}
}

// TestSegmentsFromBlocksMatchesLiveBehavior covers the Copilot-review refinements:
// non-conversation roles are skipped, server tool calls conjure no ghost card,
// an unresolved tool card replays as interrupted (not pending), and consecutive
// assistant blocks fold under one segment.
func TestSegmentsFromBlocksMatchesLiveBehavior(t *testing.T) {
	system := schema.Block{Kind: schema.KindText, Role: schema.RoleSystem, Text: &schema.TextBody{Text: "you are a harness prompt"}}
	serverCall := schema.Block{Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{ToolUseID: "srv", Name: "web_search", ToolKind: schema.ToolKindServer}}
	orphanCall := schema.Block{Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{ToolUseID: "t9", Name: "shell"}}

	segs := segmentsFromBlocks([]schema.Block{
		system,
		userText("do a thing"),
		assistantText("part one"),
		assistantText("part two"),
		serverCall,
		orphanCall,
	})

	// system text skipped; the two assistant blocks fold into one segment.
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3 (user, merged assistant, orphan tool): %+v", len(segs), segs)
	}
	if segs[0].kind != segUser {
		t.Errorf("seg0 kind = %v, want user (system text must be skipped)", segs[0].kind)
	}
	if segs[1].kind != segAssistant || !strings.Contains(segs[1].text, "part one") || !strings.Contains(segs[1].text, "part two") {
		t.Errorf("seg1 = %+v, want consecutive assistant blocks merged", segs[1])
	}
	tool := segs[2]
	if tool.kind != segTool || tool.toolName != "shell" {
		t.Fatalf("seg2 = %+v, want the client 'shell' card (server call skipped)", tool)
	}
	if !tool.toolDone || !tool.toolError {
		t.Errorf("orphan tool card = %+v, want done+error (interrupted), not pending", tool)
	}
}

// TestLaunchRehydratesTranscript covers AC2's launch path: a model built with a
// rehydrate source shows the prior turns immediately, so a `smith --resume <id>`
// start is not a blank screen.
func TestLaunchRehydratesTranscript(t *testing.T) {
	m := modelWithRehydrate(t, []schema.Block{userText("earlier question"), assistantText("earlier answer")})
	if len(m.segs) != 2 {
		t.Fatalf("launch rehydrated %d segments, want 2", len(m.segs))
	}
	if got := m.renderTranscript(); !strings.Contains(got, "earlier question") || !strings.Contains(got, "earlier answer") {
		t.Errorf("transcript missing replayed turns:\n%s", got)
	}
}

// TestResetViewRehydrates covers AC2: a session-resetting command rebuilds the
// transcript from the now-active session's blocks rather than blanking it.
func TestResetViewRehydrates(t *testing.T) {
	m := modelWithRehydrate(t, []schema.Block{userText("restored turn")})
	c := command.Command{Name: "resume", Mode: command.Inline, Run: nopHandler}
	m = update(t, m, commandDoneMsg{cmd: c, out: command.Output{Text: "Resumed.", ResetView: true}})
	last := m.segs[len(m.segs)-1]
	if last.kind != segCommand || last.text != "Resumed." {
		t.Fatalf("last segment = %+v, want the resume confirmation", last)
	}
	if got := m.renderTranscript(); !strings.Contains(got, "restored turn") {
		t.Errorf("ResetView did not replay the restored turn:\n%s", got)
	}
}

// TestResumePickerSelectionRedispatches covers AC1: a command that returns a
// Picker opens an interactive list; moving the highlight and pressing Enter
// re-dispatches the same command with the chosen item's Value, while Esc leaves
// the active session untouched.
func TestResumePickerSelectionRedispatches(t *testing.T) {
	rec := &recorder{}
	c := command.Command{Name: "resume", Mode: command.Inline, Run: rec.handler(command.Output{Text: "loaded"})}
	reg := sampleRegistry(t, c)
	m := newCommandModel(t, reg)

	out := command.Output{Picker: &command.Picker{Title: "Resume a session", Items: []command.PickerItem{
		{Label: "s1", Value: "id-1"},
		{Label: "s2", Value: "id-2"},
	}}}
	m = update(t, m, commandDoneMsg{cmd: c, out: out})
	if !m.pickerOpen() {
		t.Fatal("a Picker output did not open the picker")
	}

	// Esc cancels without re-dispatching.
	cancel := update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cancel.pickerOpen() {
		t.Error("Esc did not close the picker")
	}
	if rec.called {
		t.Error("Esc re-dispatched the command; it should leave the session untouched")
	}

	// Down then Enter chooses the second item and re-dispatches with its Value.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	mm, cmd := m.handlePickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(model)
	if m.pickerOpen() {
		t.Error("Enter did not close the picker")
	}
	runCmd(t, m, cmd)
	if !rec.called {
		t.Fatal("Enter did not re-dispatch the command")
	}
	if len(rec.args) != 1 || rec.args[0] != "id-2" {
		t.Errorf("re-dispatched with args %v, want [id-2]", rec.args)
	}
}
