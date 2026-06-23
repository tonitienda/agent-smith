package tui

import (
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// rain.go renders the AS-126 digital rain — the most memorable demo moment of
// the Matrix theme (docs/design/tui-visual-design.md §5, §7.1). It is CHROME
// ONLY: the model composites it strictly behind the splash/empty-state copy and
// never over a transcript, tool output, diff, or any data (internal/tui/CLAUDE.md
// invariant 3). The personality package answers "which intensity / is serious";
// this file owns the animation. The whole layer is deterministic given a seed so
// it is unit-testable without a live terminal.

// rainTickInterval matches the typewriter cadence (~60ms): a distinct ticker per
// cadence per the colors.go animation contract, fast enough to look like falling
// rain without thrashing the render.
const rainTickInterval = 60 * time.Millisecond

// rainTickMsg drives one rain frame; the model re-arms it while the transcript is
// still empty so the rain pauses/resumes with the empty state and /serious.
type rainTickMsg struct{}

// rainTick schedules the next rain frame.
func rainTick() tea.Cmd {
	return tea.Tick(rainTickInterval, func(time.Time) tea.Msg { return rainTickMsg{} })
}

// rainCharset is the rune pool sampled into the columns: half-width katakana
// (U+FF71–U+FF9D, single-cell in monospaced terminals — full-width katakana are
// double-cell and would jitter the columns as glyphs change, so they are never
// used), digits, and a little ASCII punctuation.
var rainCharset = []rune(
	"ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ" +
		"0123456789" + ":.\"=*+-<>|",
)

// rainHeadStyle is the bright leading cell; rainTrail fades down the green ramp,
// one darker step per row behind the head (colors.go is the single token source).
var (
	rainHeadStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorBrand)
	rainTrail     = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(ColorCommand),
		lipgloss.NewStyle().Foreground(ColorNeutral),
		lipgloss.NewStyle().Foreground(ColorMuted),
		lipgloss.NewStyle().Foreground(ColorDim),
		lipgloss.NewStyle().Foreground(ColorDimmest),
	}
)

// rainColumn is one falling stream: a head at sub-cell row y advancing by speed
// rows/tick, trailing len fading characters. chars holds one rune per terminal
// row so trail glyphs stay stable while only the head resamples each tick.
type rainColumn struct {
	x     int
	y     float64
	speed float64
	len   int
	chars []rune
}

// rain is the full animated background sized to the transcript viewport.
type rain struct {
	w, h int
	cols []rainColumn
	rng  *rand.Rand
}

// newRain builds a rain for a w×h area. seed makes the animation deterministic
// (tests pass a fixed seed; the live face seeds from the clock). Columns are
// spawned one per two terminal columns with staggered offscreen heads so the
// screen fills in gradually rather than all at once.
func newRain(w, h int, seed int64) *rain {
	r := &rain{w: w, h: h, rng: rand.New(rand.NewSource(seed))}
	for x := 0; x < w; x += 2 {
		r.cols = append(r.cols, r.spawn(x, true))
	}
	return r
}

// spawn makes a fresh column. initial staggers the head far above the top so the
// first frames fill gradually; a respawn starts just above the visible area.
func (r *rain) spawn(x int, initial bool) rainColumn {
	c := rainColumn{
		x:     x,
		speed: 0.3 + r.rng.Float64()*0.7, // [0.3, 1.0] rows/tick
		len:   4 + r.rng.Intn(13),        // [4, 16] trail length
		chars: r.randChars(r.h),
	}
	if initial {
		c.y = -r.rng.Float64() * float64(r.h*2)
	} else {
		c.y = -float64(r.rng.Intn(r.h/2 + 1))
	}
	return c
}

// randChars fills a row-indexed rune slice from the charset.
func (r *rain) randChars(n int) []rune {
	if n < 1 {
		n = 1
	}
	out := make([]rune, n)
	for i := range out {
		out[i] = rainCharset[r.rng.Intn(len(rainCharset))]
	}
	return out
}

// tick advances every column one frame: it moves the head down, resamples the
// head cell's glyph, and respawns a column once its trail has fully cleared the
// bottom so the rain runs indefinitely.
func (r *rain) tick() {
	for i := range r.cols {
		c := &r.cols[i]
		c.y += c.speed
		if head := int(c.y); head >= 0 && head < r.h {
			c.chars[head] = rainCharset[r.rng.Intn(len(rainCharset))]
		}
		if int(c.y)-c.len > r.h {
			r.cols[i] = r.spawn(c.x, false)
		}
	}
}

// rainCell is one painted grid position.
type rainCell struct {
	ch    rune
	style lipgloss.Style
	set   bool
}

// render paints the columns into a w×h grid of styled runes and returns it as h
// newline-joined rows. Unpainted cells are blanks, so the foreground copy the
// model overlays sits on a clean field of falling characters.
func (r *rain) render() string {
	if r == nil || r.w <= 0 || r.h <= 0 {
		return ""
	}
	grid := make([][]rainCell, r.h)
	for i := range grid {
		grid[i] = make([]rainCell, r.w)
	}
	for _, c := range r.cols {
		if c.x < 0 || c.x >= r.w {
			continue
		}
		head := int(c.y)
		for d := 0; d <= c.len; d++ {
			row := head - d
			if row < 0 || row >= r.h {
				continue
			}
			style := rainHeadStyle
			if d > 0 {
				idx := d - 1
				if idx >= len(rainTrail) {
					idx = len(rainTrail) - 1
				}
				style = rainTrail[idx]
			}
			grid[row][c.x] = rainCell{ch: c.chars[row], style: style, set: true}
		}
	}
	var b strings.Builder
	for y := 0; y < r.h; y++ {
		for x := 0; x < r.w; x++ {
			if cell := grid[y][x]; cell.set {
				b.WriteString(cell.style.Render(string(cell.ch)))
			} else {
				b.WriteByte(' ')
			}
		}
		if y < r.h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
