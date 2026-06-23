package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/personality"
)

// ansiRE matches SGR escape sequences so assertions can read the plain text
// regardless of the active lipgloss color profile.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestRainRenderFillsAndStaysInBounds checks the rain paints green characters
// and never emits more rows than its height — the empty-state composite relies
// on a fixed-height grid (AS-126).
func TestRainRenderFillsAndStaysInBounds(t *testing.T) {
	r := newRain(20, 8, 42)
	for i := 0; i < 40; i++ { // let columns descend into view
		r.tick()
	}
	out := r.render()
	rows := strings.Split(out, "\n")
	if len(rows) != 8 {
		t.Fatalf("rain rendered %d rows, want 8", len(rows))
	}
	if strings.TrimSpace(stripANSI(out)) == "" {
		t.Fatal("rain rendered nothing after settling; expected falling characters")
	}
}

// TestRainDeterministicBySeed guards reproducibility: the same seed and tick
// count must produce identical frames, which is what makes the animation
// unit-testable without a live terminal.
func TestRainDeterministicBySeed(t *testing.T) {
	a, b := newRain(16, 6, 7), newRain(16, 6, 7)
	for i := 0; i < 20; i++ {
		a.tick()
		b.tick()
	}
	if a.render() != b.render() {
		t.Fatal("same seed produced different frames")
	}
}

// TestSeriousModeProducesNoRainOrFlavor is the AS-126 acceptance test: with the
// kill switch on, the rendered empty state shows no rain, no idle phrase, and no
// Matrix user name — only the plain splash.
func TestSeriousModeProducesNoRainOrFlavor(t *testing.T) {
	serious := personality.New(personality.Settings{Theme: "matrix", SeriousMode: ptrBool(true)}, true)
	m := newModel(&fakeRunner{}, staticMeta(Meta{Model: "m"}), make(chan loop.UIEvent), nil, nil, nil, true, nil, nil, nil)
	m.pers = serious
	m = m.resize(40, 12)

	if m.rainActive() {
		t.Fatal("rainActive() true under serious mode")
	}
	if got := m.idlePhrase(); got != "" {
		t.Fatalf("idlePhrase() = %q under serious mode, want empty", got)
	}
	if got := m.userLabel(); got != "you" {
		t.Fatalf("userLabel() = %q under serious mode, want plain \"you\"", got)
	}

	// A rain frame must not be composited: even after a tick attempt the empty
	// state is just the plain splash (no rain grid rows beyond the copy).
	view := stripANSI(m.renderTranscript())
	if strings.Contains(view, "Mr. Anderson") {
		t.Fatal("serious empty state leaked a Matrix name")
	}
	if !strings.Contains(view, "Ask Agent Smith anything to begin.") {
		t.Fatalf("serious empty state missing plain invite: %q", view)
	}
}

// TestMediumIntensityRendersRainAndNames confirms the default (medium) path turns
// the chrome on: the rain animates, the idle phrase rotates, and the user label
// renames.
func TestMediumIntensityRendersRainAndNames(t *testing.T) {
	pers := personality.New(personality.Settings{Theme: "matrix", SeriousMode: ptrBool(false)}, true)
	m := newModel(&fakeRunner{}, staticMeta(Meta{Model: "m"}), make(chan loop.UIEvent), nil, nil, nil, true, nil, nil, nil)
	m.pers = pers
	m = m.resize(40, 12)

	if !m.rainActive() {
		t.Fatal("rainActive() false at default medium intensity on the empty screen")
	}
	if m.idlePhrase() == "" {
		t.Fatal("idlePhrase() empty at medium intensity")
	}
	if got := m.userLabel(); got != "Mr. Anderson" {
		t.Fatalf("userLabel() = %q, want Mr. Anderson at medium intensity", got)
	}

	// Once a turn is recorded the rain retires (never over content).
	m.segs = append(m.segs, segment{kind: segUser, text: "hi", done: true})
	if m.rainActive() {
		t.Fatal("rainActive() true after a turn began; rain must stop over content")
	}
}

func ptrBool(b bool) *bool { return &b }
