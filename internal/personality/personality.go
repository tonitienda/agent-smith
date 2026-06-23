// Package personality is the Matrix layer (AS-053, PRD §7.21, Appendix D): the
// optional cosmetic theme — rotating themed status lines and Matrix role
// display-names — plus the /serious kill switch that mutes it.
//
// The engineering substance is the containment guarantee, not the jokes: this
// package is the single place flavor strings live, and the substance-producing
// paths (the loop, providers, tools, cost/eventlog, schema) have no import path
// to it, so no pun can ever leak into generated code, diffs, commits, file
// writes, error payloads, or programmatic output (PRD §7.21). That boundary is
// enforced by no_business_imports_test.go, not just review.
//
// Flavor is confined to chrome — spinners, the status line, and entity
// display-names. A face asks a *Personality for a themed status line or a role
// name; with the theme off, serious mode on, or a non-interactive face, every
// answer is the plain, substance-first text.
package personality

import (
	"strings"
	"sync"
	"time"
)

// Role identifies an entity whose display-name the theme renames (Appendix D).
type Role string

// The roles the Matrix name map covers (Appendix D). New roles are additive: an
// unknown role falls back to its plain label, so callers never break (PRD D2).
const (
	RoleUser             Role = "user"
	RoleRouter           Role = "router"
	RoleSkillAnalyzer    Role = "skill_expectation_analyzer"
	RoleInsightsWriter   Role = "insights_writer"
	RoleBackgroundRunner Role = "background_runner"
	RoleSystemSubagents  Role = "system_subagents"
)

const (
	themeMatrix = "matrix"
	themeNone   = "none"
)

// Intensity is the personality flavor dial (Appendix D, AS-126). Levels are
// additive: each adds chrome flavor on top of the one below it. The default is
// IntensityMedium — the full phosphor-rain effect on splash/idle is the most
// memorable demo moment and is instantly reversible via /serious.
type Intensity int

const (
	// IntensitySubtle themes status/loading lines only — no renaming, no rain.
	IntensitySubtle Intensity = iota
	// IntensityMedium adds digital rain on idle/splash, rotating idle phrases,
	// Matrix role names, and the one-shot logo glitch-in.
	IntensityMedium
	// IntensityBold layers on scanlines/CRT sweep/darker canvas (render-side,
	// reserved for later TUI tickets).
	IntensityBold
)

// parseIntensity resolves the config string to an Intensity, defaulting to
// medium. "full" is kept as an accepted alias for "medium" so configs written
// against the AS-053 two-value (full|subtle) field still parse (PRD D2); any
// unrecognized value also resolves to the medium default rather than failing.
func parseIntensity(s string) Intensity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "subtle":
		return IntensitySubtle
	case "bold":
		return IntensityBold
	default: // medium, full (alias), empty, or anything unrecognized
		return IntensityMedium
	}
}

// statusRotateSeconds is how long each themed status line shows before the next
// is picked. Rotating on a few-second bucket changes the line while the agent
// works without per-render flicker and without storing any rotation state.
const statusRotateSeconds = 3

// plainStatusLine is the substance-first working text shown when the theme is
// off, serious mode is on, or the face is non-interactive.
const plainStatusLine = "working…"

// plainNames are the substance-first labels used when the theme is muted. They
// are not flavor, so they remain the fallback for every code path.
var plainNames = map[Role]string{
	RoleUser:             "you",
	RoleRouter:           "router",
	RoleSkillAnalyzer:    "skill analyzer",
	RoleInsightsWriter:   "insights",
	RoleBackgroundRunner: "background runner",
	RoleSystemSubagents:  "sub-agents",
}

// matrixNames are the themed display-names (Appendix D). Flavor — confined here.
var matrixNames = map[Role]string{
	RoleUser:             "Mr. Anderson",
	RoleRouter:           "The Keymaker",
	RoleSkillAnalyzer:    "The Oracle",
	RoleInsightsWriter:   "The Architect",
	RoleBackgroundRunner: "Sentinels",
	RoleSystemSubagents:  "Agents",
}

// matrixStatusLines rotate while the agent works (Appendix D). Flavor — confined
// here.
var matrixStatusLines = []string{
	"entering the matrix…",
	"dodging bullets…",
	"following the white rabbit…",
	"there is no spoon…",
	"asking the Oracle…",
	"bending the spoon…",
	"the matrix has you…",
	"knock, knock, neo…",
	"free your mind…",
	"what is real?",
}

// Settings is the Appendix D personality config, decoded from the `personality`
// config section (AS-031). All fields are optional; absent fields take their
// defaults. SeriousMode is a pointer so an unset value can be told apart from an
// explicit false and resolved against the face (off in the TUI, on for
// non-interactive faces).
type Settings struct {
	Theme       string            `json:"theme"`        // matrix | none (default matrix)
	SeriousMode *bool             `json:"serious_mode"` // nil => face default
	Intensity   string            `json:"intensity"`    // subtle | medium | bold (default medium; "full" = medium)
	Names       map[string]string `json:"names"`        // role -> override display-name
}

// Personality renders the theme for one session. It is safe for concurrent use:
// the status line is read from a render goroutine while /serious toggles it from
// the command goroutine.
type Personality struct {
	mu        sync.RWMutex
	serious   bool
	theme     string
	intensity Intensity // subtle => status/loading only; medium/bold add renaming + rain
	overrides map[Role]string
}

// New builds a Personality from settings for a face. interactive selects the
// serious-mode default when settings.SeriousMode is unset: the theme is on in
// the interactive TUI (serious off) and off automatically for non-interactive
// faces — headless/ACP/CI/async — so programmatic output stays clean (§7.21).
func New(s Settings, interactive bool) *Personality {
	p := &Personality{
		theme:     normalizeTheme(s.Theme),
		intensity: parseIntensity(s.Intensity),
		overrides: map[Role]string{},
	}
	for k, v := range s.Names {
		if name := strings.TrimSpace(v); name != "" {
			p.overrides[Role(k)] = name
		}
	}
	if s.SeriousMode != nil {
		p.serious = *s.SeriousMode
	} else {
		p.serious = !interactive
	}
	return p
}

// normalizeTheme defaults an unset theme to matrix and treats any value other
// than the explicit "none" as matrix, so a typo themes rather than silently
// disabling (the kill switch is serious_mode, not a misspelled theme).
func normalizeTheme(theme string) string {
	if strings.EqualFold(strings.TrimSpace(theme), themeNone) {
		return themeNone
	}
	return themeMatrix
}

// themed reports whether flavor should show: the Matrix theme is selected and
// serious mode is off. The caller must hold at least a read lock.
func (p *Personality) themed() bool {
	return p.theme == themeMatrix && !p.serious
}

// Name returns the display-name for a role: the themed Matrix name (or a
// configured override) when the theme is active at full intensity, otherwise the
// plain, substance-first label. An unknown role falls back to its raw string.
func (p *Personality) Name(r Role) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// Subtle intensity confines flavor to status/loading lines — no renaming — so
	// names stay plain even when the theme is otherwise on (Appendix D). Renaming
	// is gated to medium/bold.
	if p.themed() && p.intensity >= IntensityMedium {
		if n, ok := p.overrides[r]; ok {
			return n
		}
		if n, ok := matrixNames[r]; ok {
			return n
		}
	}
	if n, ok := plainNames[r]; ok {
		return n
	}
	return string(r)
}

// StatusLine returns the working-state line for the status bar: a rotating
// themed line while the theme is active, otherwise the plain "working…". It is
// pure and stateless — the rotation is a function of the clock — so a face can
// call it every render.
func (p *Personality) StatusLine() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.themed() {
		return plainStatusLine
	}
	// Modulo on the int64 before narrowing avoids a 32-bit overflow, and the
	// negative-bucket guard keeps a clock set before the Unix epoch from yielding
	// a negative (out-of-bounds) index.
	bucket := time.Now().Unix() / statusRotateSeconds
	if bucket < 0 {
		bucket = -bucket
	}
	return matrixStatusLines[int(bucket%int64(len(matrixStatusLines)))]
}

// Intensity returns the effective flavor intensity for chrome render decisions
// (digital rain, idle phrases, glitch-in). While the theme is muted — serious
// mode on, or theme "none" — it resolves to IntensitySubtle so chrome that gates
// those effects on >= IntensityMedium goes quiet instantly, matching the kill
// switch's reach over names and status lines.
func (p *Personality) Intensity() Intensity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.themed() {
		return IntensitySubtle
	}
	return p.intensity
}

// Serious reports whether the kill switch is on (all flavor muted).
func (p *Personality) Serious() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.serious
}

// SetSerious sets the kill switch. true mutes every pun/reference globally with
// no residual flavor; false restores the theme (when one is selected).
func (p *Personality) SetSerious(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.serious = v
}

// ToggleSerious flips the kill switch and returns the new serious state, backing
// the runtime /serious command.
func (p *Personality) ToggleSerious() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.serious = !p.serious
	return p.serious
}
