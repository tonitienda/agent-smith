// Package codingskills ships the Coding Mode process skill pack (AS-074): the
// opinionated, per-phase skills that make the advisor actually advise, bundled in
// the binary and auto-enabled per phase via the ordinary skills model (AS-034,
// D-CODE-5.2/-6). They are not installed or pulled — they travel with Smith, so
// Coding Mode works with zero setup (D-CODE-6).
//
// Each skill is an ordinary skill.Skill parsed from an embedded SKILL.md, so a
// bundled skill is indistinguishable from a hand-written one: a project can
// shadow or disable any of them by name (AS-075) without touching the mode core.
// The phase→skill mapping lives next to the phase definitions (internal/mode,
// AS-072); this package only owns the bodies and the grounding discipline they
// must honor (D-CODE-8).
package codingskills

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"

	"github.com/tonitienda/agent-smith/internal/skill"
)

// Scope labels where these skills came from when surfaced through the skill
// model — distinct from "user"/"project" so a face can tell a bundled process
// skill from a discovered one.
const Scope = "bundled"

//go:embed skills
var bundled embed.FS

// Pack returns the bundled process skills, parsed from the embedded manifests
// with the same rules as on-disk skills. The result is sorted by name. It is
// cheap to call but the caller typically scans once at startup and reuses the
// snapshot, mirroring how user/project skills are loaded.
func Pack() ([]skill.Skill, error) {
	sub, err := fs.Sub(bundled, "skills")
	if err != nil {
		return nil, fmt.Errorf("open bundled skills: %w", err)
	}
	skills, err := skill.LoadFS(sub, Scope)
	if err != nil {
		return nil, fmt.Errorf("load bundled skills: %w", err)
	}
	return skills, nil
}

// groundingSignals matches the concrete references a Coding Mode finding must
// carry (D-CODE-8: cite the file, function, missing test, or ticket — never
// "consider best practices"). A finding is grounded when it names at least one of
// these. The patterns are deliberately conservative so generic advice ("improve
// error handling") fails while a real reference passes.
var groundingSignals = []*regexp.Regexp{
	regexp.MustCompile("`[^`]+`"),                     // a backticked code span
	regexp.MustCompile(`\bAS-\d+\b`),                  // a ticket reference
	regexp.MustCompile(`[\w./-]+\.[A-Za-z]{2,}[\w]*`), // a file path with an extension (foo/bar.go); {2,} so "e.g."/"i.e." are not mistaken for one
	regexp.MustCompile(`\b[A-Za-z_]\w*\([^)]*\)`),     // a function/method call: Foo(), pkg.Bar(x)
	regexp.MustCompile(`(?:^|[\s(])[\w./-]+:\d+\b`),   // a file:line span
}

// IsGrounded reports whether a finding cites something concrete — a file,
// symbol, span, or ticket — rather than generic advice (D-CODE-8). It is the
// machine-checkable form of the evidence discipline the process skills demand and
// the same standard /insights holds itself to. The runtime and tests use it to
// keep the pack honest: a skill that emits "follow best practices" is not doing
// its job.
func IsGrounded(finding string) bool {
	for _, re := range groundingSignals {
		if re.MatchString(finding) {
			return true
		}
	}
	return false
}
