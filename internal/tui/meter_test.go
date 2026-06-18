package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/loop"
)

func TestMeterRenderKnownWindow(t *testing.T) {
	got := Meter{Tokens: 12300, Window: 200000, CostUSD: 0.0421, CostKnown: true}.render()
	for _, want := range []string{"12.3k", "200k", "6%", "$0.0421", "░"} {
		if !strings.Contains(got, want) {
			t.Errorf("meter %q missing %q", got, want)
		}
	}
}

func TestMeterRenderUnknownWindow(t *testing.T) {
	got := Meter{Tokens: 1500, Window: 0, CostUSD: 0.01, CostKnown: true}.render()
	if !strings.Contains(got, "1.5k tok") {
		t.Errorf("meter %q should show a bare token count when the window is unknown", got)
	}
	if strings.Contains(got, "%") || strings.Contains(got, "█") {
		t.Errorf("meter %q should have no percentage or bar without a window", got)
	}
}

func TestMeterRenderUnknownCost(t *testing.T) {
	got := Meter{Tokens: 100, Window: 200000, CostKnown: false}.render()
	if !strings.Contains(got, "$?") {
		t.Errorf("meter %q should mark an unpriced session cost as unknown", got)
	}
}

func TestMeterEmptyRendersNothing(t *testing.T) {
	if got := (Meter{}).render(); got != "" {
		t.Errorf("empty meter rendered %q, want \"\"", got)
	}
}

func TestMeterColorThresholds(t *testing.T) {
	// Assert the gauge color directly (not the rendered string, which also differs
	// by bar fill and percentage text) so the test actually pins the thresholds:
	// green < 60%, yellow < 85%, red beyond — including the exact boundaries.
	cases := []struct {
		pct  float64
		want lipgloss.Color
	}{
		{0, "10"}, {59, "10"}, {60, "11"}, {84, "11"}, {85, "9"}, {130, "9"},
	}
	for _, c := range cases {
		if got := meterStyle(c.pct).GetForeground(); got != c.want {
			t.Errorf("meterStyle(%.0f) color = %v, want %v", c.pct, got, c.want)
		}
	}
}

func TestMeterCurrencyPrefix(t *testing.T) {
	got := Meter{Tokens: 10, Window: 100, CostUSD: 1.5, CostKnown: true, Currency: "EUR "}.render()
	if !strings.Contains(got, "EUR 1.5000") {
		t.Errorf("meter %q should format cost with the currency prefix", got)
	}
	unknown := Meter{Tokens: 10, Window: 100, Currency: "EUR "}.render()
	if !strings.Contains(unknown, "EUR ?") {
		t.Errorf("meter %q should mark unknown cost with the currency prefix", unknown)
	}
}

func TestMeterBarFillsWithUsage(t *testing.T) {
	empty := meterBar(0)
	full := meterBar(100)
	if strings.Contains(empty, "█") {
		t.Errorf("0%% bar %q should have no fill", empty)
	}
	if strings.Contains(full, "░") {
		t.Errorf("100%% bar %q should be fully filled", full)
	}
	if len([]rune(empty)) != meterBarWidth || len([]rune(full)) != meterBarWidth {
		t.Errorf("bar width changed: empty=%d full=%d, want %d", len([]rune(empty)), len([]rune(full)), meterBarWidth)
	}
}

func TestHumanTokens(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1500: "1.5k", 200000: "200k", 1047576: "1M"}
	for n, want := range cases {
		if got := humanTokens(n); got != want {
			t.Errorf("humanTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestMeterReceivesActiveModel checks the model is threaded into the MeterFunc so
// the window denominator can rescale on a model switch (AS-023 /model).
func TestMeterReceivesActiveModel(t *testing.T) {
	var got string
	m := newModel(&fakeRunner{}, staticMeta(Meta{Model: "claude-opus-4-8"}),
		make(chan loop.UIEvent), nil, nil, func(model string) Meter {
			got = model
			return Meter{}
		}, false, nil, nil)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	_ = m
	if got != "claude-opus-4-8" {
		t.Errorf("MeterFunc got model %q, want the status line's active model", got)
	}
}

// TestMeterShownInStatusLineAndUpdates wires a MeterFunc whose value the test
// controls, and checks the meter is visible in the view and refreshes within one
// event (AS-025 acceptance: always visible, updates within one event).
func TestMeterShownInStatusLineAndUpdates(t *testing.T) {
	meter := Meter{Tokens: 1000, Window: 200000, CostUSD: 0.5, CostKnown: true}
	m := newModel(&fakeRunner{}, staticMeta(Meta{Provider: "anthropic", Model: "claude-opus-4-8", Session: "s"}),
		make(chan loop.UIEvent), nil, nil, func(string) Meter { return meter }, false, nil, nil)
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})

	if !strings.Contains(m.View(), "$0.5000") {
		t.Fatalf("initial view missing meter cost:\n%s", m.View())
	}

	// A new event must pull the updated snapshot into the status line.
	meter = Meter{Tokens: 150000, Window: 200000, CostUSD: 1.25, CostKnown: true}
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnComplete})
	view := m.View()
	for _, want := range []string{"150k", "75%", "$1.2500"} {
		if !strings.Contains(view, want) {
			t.Errorf("view after event missing %q:\n%s", want, view)
		}
	}
}
