package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
)

// space builds the space KeyMsg the selector toggles on.
func space() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeySpace} }

// openTestSelector dispatches a /clean command returning sel and runs the
// resulting command so the model opens the interactive selector.
func openTestSelector(t *testing.T, sel command.Selector) model {
	t.Helper()
	reg := command.NewRegistry()
	if err := reg.Register(command.Command{
		Name: "clean",
		Mode: command.FullScreen,
		Run: func(_ context.Context, _ []string) (command.Output, error) {
			return command.Output{Selector: &sel}, nil
		},
	}); err != nil {
		t.Fatalf("register clean: %v", err)
	}
	m := newCommandModel(t, reg)
	cmd, ok := reg.Lookup("clean")
	if !ok {
		t.Fatal("clean not registered")
	}
	m = runCmd(t, m, runCommand(cmd, nil))
	if !m.selectorOpen() {
		t.Fatal("selector did not open")
	}
	return m
}

// TestSelectorTogglesAndPreviews covers AC1: items toggle without typing handles
// and the selection drives the live preview shown in the footer.
func TestSelectorTogglesAndPreviews(t *testing.T) {
	var previewed [][]string
	sel := command.Selector{
		Title: "Clean",
		Items: []command.SelectItem{
			{Label: "blk_a · user · 100 tok", Value: "blk_a"},
			{Label: "blk_b · assistant · 200 tok", Value: "blk_b"},
		},
		Preview: func(values []string) command.SelectPreview {
			previewed = append(previewed, values)
			if len(values) == 0 {
				return command.SelectPreview{Summary: "Nothing selected"}
			}
			return command.SelectPreview{Summary: "Selected " + strings.Join(values, ",")}
		},
	}
	m := openTestSelector(t, sel)

	// The cursor starts on the first item; space selects it and refreshes preview.
	m = update(t, m, space())
	if !m.selector.checked["blk_a"] {
		t.Fatal("space did not select the first item")
	}
	if got := m.selector.preview.Summary; got != "Selected blk_a" {
		t.Fatalf("preview after select = %q, want %q", got, "Selected blk_a")
	}

	// Move to the second item and select it too.
	m = update(t, m, key("down"))
	m = update(t, m, space())
	if !m.selector.checked["blk_b"] {
		t.Fatal("space did not select the second item")
	}
	if got := m.selector.preview.Summary; got != "Selected blk_a,blk_b" {
		t.Fatalf("preview after two selects = %q", got)
	}

	// Space again on blk_b deselects it.
	m = update(t, m, space())
	if m.selector.checked["blk_b"] {
		t.Fatal("space did not deselect blk_b")
	}

	// The footer shows the live preview summary (AC1).
	if !strings.Contains(m.selectorFooter(), "Selected blk_a") {
		t.Fatalf("footer missing live preview: %q", m.selectorFooter())
	}
}

// TestSelectorApply covers AC2: Enter applies the checked selection through the
// Apply closure and closes the surface, surfacing the result inline.
func TestSelectorApply(t *testing.T) {
	var applied []string
	sel := command.Selector{
		Items: []command.SelectItem{
			{Label: "blk_a", Value: "blk_a"},
			{Label: "blk_b", Value: "blk_b"},
		},
		Preview: func([]string) command.SelectPreview { return command.SelectPreview{} },
		Apply: func(values []string) string {
			applied = values
			return "Removed 1 segment"
		},
	}
	m := openTestSelector(t, sel)
	m = update(t, m, space())      // select blk_a
	m = update(t, m, key("enter")) // apply

	if m.selectorOpen() {
		t.Fatal("selector should close after applying")
	}
	if len(applied) != 1 || applied[0] != "blk_a" {
		t.Fatalf("Apply got %v, want [blk_a]", applied)
	}
	if last := m.segs[len(m.segs)-1]; last.text != "Removed 1 segment" {
		t.Fatalf("result not surfaced inline: %q", last.text)
	}
}

// TestSelectorApplyEmptyIsNoOp covers the guard: Enter with nothing selected does
// not call Apply or append an empty removal.
func TestSelectorApplyEmptyIsNoOp(t *testing.T) {
	called := false
	sel := command.Selector{
		Items:   []command.SelectItem{{Label: "blk_a", Value: "blk_a"}},
		Preview: func([]string) command.SelectPreview { return command.SelectPreview{} },
		Apply:   func([]string) string { called = true; return "x" },
	}
	m := openTestSelector(t, sel)
	m = update(t, m, key("enter"))
	if called {
		t.Fatal("Apply called with no selection")
	}
	if !m.selectorOpen() {
		t.Fatal("selector closed on an empty apply")
	}
}

// TestSelectorRestore covers AC3: a single archived block is restored from the
// archive section, via Enter on its row, through the Restore closure.
func TestSelectorRestore(t *testing.T) {
	var restored string
	sel := command.Selector{
		Items:   []command.SelectItem{{Label: "blk_live", Value: "blk_live"}},
		Archive: []command.SelectItem{{Label: "blk_gone", Value: "blk_gone"}},
		Preview: func([]string) command.SelectPreview { return command.SelectPreview{} },
		Restore: func(value string) string { restored = value; return "Restored 1 segment" },
	}
	m := openTestSelector(t, sel)

	// Move past the one live item onto the archive row, then restore it.
	m = update(t, m, key("down"))
	m = update(t, m, key("enter"))

	if restored != "blk_gone" {
		t.Fatalf("Restore got %q, want blk_gone", restored)
	}
	if m.selectorOpen() {
		t.Fatal("selector should close after a restore")
	}
	if last := m.segs[len(m.segs)-1]; last.text != "Restored 1 segment" {
		t.Fatalf("restore result not surfaced inline: %q", last.text)
	}
}

// TestSelectorEscCloses covers the cancel path: Esc closes the surface without
// applying or restoring anything.
func TestSelectorEscCloses(t *testing.T) {
	sel := command.Selector{
		Items:   []command.SelectItem{{Label: "blk_a", Value: "blk_a"}},
		Apply:   func([]string) string { t.Fatal("Apply must not run on Esc"); return "" },
		Preview: func([]string) command.SelectPreview { return command.SelectPreview{} },
	}
	m := openTestSelector(t, sel)
	m = update(t, m, key("esc"))
	if m.selectorOpen() {
		t.Fatal("Esc did not close the selector")
	}
}
