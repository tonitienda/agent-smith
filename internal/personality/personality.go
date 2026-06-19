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
}

// Settings is the Appendix D personality config, decoded from the `personality`
// config section (AS-031). All fields are optional; absent fields take their
// defaults. SeriousMode is a pointer so an unset value can be told apart from an
// explicit false and resolved against the face (off in the TUI, on for
// non-interactive faces).
type Settings struct {
	Theme       string            `json:"theme"`        // matrix | none (default matrix)
	SeriousMode *bool             `json:"serious_mode"` // nil => face default
	Intensity   string            `json:"intensity"`    // full | subtle (default full)
	Names       map[string]string `json:"names"`        // role -> override display-name
}

// Personality renders the theme for one session. It is safe for concurrent use:
// the status line is read from a render goroutine while /serious toggles it from
// the command goroutine.
type Personality struct {
	mu        sync.RWMutex
	serious   bool
	theme     string
	subtle    bool // intensity: subtle => status/loading only, no renaming
	overrides map[Role]string
}

// New builds a Personality from settings for a face. interactive selects the
// serious-mode default when settings.SeriousMode is unset: the theme is on in
// the interactive TUI (serious off) and off automatically for non-interactive
// faces — headless/ACP/CI/async — so programmatic output stays clean (§7.21).
func New(s Settings, interactive bool) *Personality {
	p := &Personality{
		theme:     normalizeTheme(s.Theme),
		subtle:    strings.EqualFold(strings.TrimSpace(s.Intensity), "subtle"),
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
	// names stay plain even when the theme is otherwise on (Appendix D).
	if p.themed() && !p.subtle {
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
