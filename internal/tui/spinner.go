package tui

// brailleSpinnerFrames is the single braille spinner glyph cycle for every
// in-flight indicator in the TUI: tool cards (AS-124), the status-line running
// state (AS-125), and the agents panel (AS-130). Define the cycle once here and
// reference it — never inline a parallel set of glyphs at a call site.
//
// Frames advance off the shared bubbles spinner.TickMsg cadence (~110ms) so no
// second ticker is introduced (the colors.go animation-tick contract).
var brailleSpinnerFrames = []rune("⣾⣽⣻⢿⣿⡿⣟⣯")

// brailleSpinnerFrame returns the glyph for tick index i, wrapping around the
// cycle. Negative indices are tolerated so a caller need not clamp its counter.
func brailleSpinnerFrame(i int) string {
	n := len(brailleSpinnerFrames)
	return string(brailleSpinnerFrames[((i%n)+n)%n])
}
