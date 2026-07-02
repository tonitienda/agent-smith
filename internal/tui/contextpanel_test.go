package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/command"
)

// sampleContextView is a fully-populated view used across the render tests.
func sampleContextView() command.ContextView {
	return command.ContextView{
		Segments: []command.ContextSegment{
			{Label: "system+memory", Tokens: 40_000},
			{Label: "tool result", Tokens: 30_000},
			{Label: "assistant", Tokens: 20_000},
			{Label: "file read", Tokens: 10_000},
		},
		Used:             100_000,
		Window:           200_000,
		CompactThreshold: 180_000,
		CostUSD:          0.42,
		Priced:           true,
		Currency:         "$",
		CacheReadTokens:  50_000,
		CacheWriteTokens: 10_000,
		CacheHitRate:     0.5,
		CacheKnown:       true,
		MemoryLoaded:     true,
	}
}

// barLine returns the segmented-bar row (the first line built only of █ fills),
// with ANSI stripped, so a test can measure its visible width.
func barLine(t *testing.T, panel string) string {
	t.Helper()
	for _, line := range strings.Split(panel, "\n") {
		plain := stripANSI(line)
		if plain != "" && strings.Trim(plain, "█") == "" {
			return plain
		}
	}
	t.Fatalf("no segmented-bar line found in:\n%s", panel)
	return ""
}

func TestContextBarWidthMatchesPanel(t *testing.T) {
	v := sampleContextView()
	for _, width := range []int{80, 120, 200} {
		panel := renderContextPanel(v, width)
		bar := barLine(t, panel)
		if got := lipgloss.Width(bar); got != width {
			t.Errorf("width %d: bar visible width = %d, want %d\nbar=%q", width, got, width, bar)
		}
	}
}

func TestContextBarProportional(t *testing.T) {
	v := sampleContextView()
	// On a 100-col bar over a 200k window: system+memory 40k→20, tool 30k→15,
	// assistant 20k→10, file 10k→5, free 100k→50.
	_, cells := barRuns(v, 100)
	want := []int{20, 15, 10, 5, 50}
	if len(cells) != len(want) {
		t.Fatalf("run count = %d, want %d (%v)", len(cells), len(want), cells)
	}
	sum := 0
	for i, n := range cells {
		if n != want[i] {
			t.Errorf("run %d cells = %d, want %d", i, n, want[i])
		}
		sum += n
	}
	if sum != 100 {
		t.Errorf("cells sum = %d, want 100", sum)
	}
}

func TestContextBarRunsSumToWidth(t *testing.T) {
	// Awkward token splits must still fill the bar exactly (largest-remainder).
	v := command.ContextView{
		Segments: []command.ContextSegment{
			{Label: "a", Tokens: 333}, {Label: "b", Tokens: 333}, {Label: "c", Tokens: 334},
		},
		Used: 1000, Window: 1000,
	}
	for _, w := range []int{7, 40, 81, 100, 137} {
		_, cells := barRuns(v, w)
		sum := 0
		for _, n := range cells {
			sum += n
		}
		if sum != w {
			t.Errorf("width %d: cells sum = %d, want %d (%v)", w, sum, w, cells)
		}
	}
}

func TestContextBarOverBudget(t *testing.T) {
	// Used exceeds Window: the bar must still be exactly barWidth wide (no overflow)
	// and carry no free space.
	v := command.ContextView{
		Segments: []command.ContextSegment{
			{Label: "assistant", Tokens: 150_000},
			{Label: "tool result", Tokens: 120_000},
		},
		Used:   270_000,
		Window: 200_000,
	}
	colors, cells := barRuns(v, 100)
	sum := 0
	for _, n := range cells {
		sum += n
	}
	if sum != 100 {
		t.Errorf("over-budget bar cells sum = %d, want 100 (%v)", sum, cells)
	}
	// No free run should be emitted (free space is zero when over budget).
	for i, c := range colors {
		if c == ColorFree && cells[i] > 0 {
			t.Errorf("unexpected free cells when over budget: run %d has %d", i, cells[i])
		}
	}
	if got := lipgloss.Width(barLine(t, renderContextPanel(v, 100))); got != 100 {
		t.Errorf("over-budget panel bar width = %d, want 100", got)
	}
}

func TestContextLegendResponsive(t *testing.T) {
	v := sampleContextView()
	// Narrow panel → one legend cell per line (single column).
	narrow := stripANSI(legendGrid(v, 50))
	for _, line := range strings.Split(narrow, "\n") {
		if strings.Count(line, "█") > 1 {
			t.Errorf("narrow legend line has >1 column: %q", line)
		}
	}
	// Wide panel → two cells per line where segments allow it.
	wide := stripANSI(legendGrid(v, 120))
	twoCol := false
	for _, line := range strings.Split(wide, "\n") {
		if strings.Count(line, "█") == 2 {
			twoCol = true
		}
	}
	if !twoCol {
		t.Errorf("wide legend never used two columns:\n%s", wide)
	}
}

func TestContextCompactMarker(t *testing.T) {
	v := sampleContextView()
	// used (100k) < 0.9*window (180k): the panel still shows the marker at the
	// threshold. The label text must be present.
	panel := renderContextPanel(v, 120)
	if !strings.Contains(stripANSI(panel), "auto-compact 180k") {
		t.Errorf("expected auto-compact marker label, got:\n%s", panel)
	}

	// With the window unknown there is no column to place the marker at.
	v.Window = 0
	v.CompactThreshold = 0
	panel = renderContextPanel(v, 120)
	if strings.Contains(stripANSI(panel), "auto-compact") {
		t.Errorf("expected no marker when window unknown, got:\n%s", panel)
	}
}

func TestContextStatsRailCostColor(t *testing.T) {
	v := sampleContextView()
	rail := statsRail(v)
	// The cost figure must render in the cost colour (ColorCommand). Confirm the
	// styled cost string appears verbatim.
	wantCost := StyleCost.Render("$0.42")
	if !strings.Contains(rail, wantCost) {
		t.Errorf("stats rail missing cost in cost colour.\nwant substring: %q\ngot:\n%s", wantCost, rail)
	}
}

func TestContextGracefulDegradation(t *testing.T) {
	// No window, no pricing, no cache: every derived figure degrades to a
	// placeholder rather than a misleading zero.
	v := command.ContextView{
		Segments: []command.ContextSegment{{Label: "assistant", Tokens: 1200}},
		Used:     1200,
	}
	panel := stripANSI(renderContextPanel(v, 80))
	rail := stripANSI(statsRail(v))
	if !strings.Contains(rail, contextPlaceholder) {
		t.Errorf("expected placeholders in stats rail, got:\n%s", rail)
	}
	if strings.Contains(panel, "auto-compact") {
		t.Errorf("no marker expected without a window, got:\n%s", panel)
	}
	// The bar still renders (scaled to the live total) at the requested width.
	if got := lipgloss.Width(barLine(t, renderContextPanel(v, 80))); got != 80 {
		t.Errorf("degraded bar width = %d, want 80", got)
	}
}

func TestContextEmptyLegend(t *testing.T) {
	v := command.ContextView{Window: 200_000}
	panel := stripANSI(renderContextPanel(v, 80))
	if !strings.Contains(panel, "empty") {
		t.Errorf("expected empty-context note, got:\n%s", panel)
	}
}
