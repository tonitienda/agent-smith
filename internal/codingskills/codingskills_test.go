package codingskills

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/mode"
)

// TestPackShipsBundledSkills asserts the process pack travels in the binary and
// covers every skill the phase definitions declare — so Coding Mode works with no
// install step (AS-074 acceptance: ships with the binary; D-CODE-6).
func TestPackShipsBundledSkills(t *testing.T) {
	pack, err := Pack()
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}
	got := map[string]bool{}
	for _, s := range pack {
		if s.Scope != Scope {
			t.Errorf("skill %q scope = %q, want %q", s.Name, s.Scope, Scope)
		}
		if strings.TrimSpace(s.Body) == "" {
			t.Errorf("skill %q has an empty body", s.Name)
		}
		if strings.TrimSpace(s.Description) == "" {
			t.Errorf("skill %q has no description for the model", s.Name)
		}
		got[s.Name] = true
	}

	// Every skill named by a phase definition must exist in the pack (or the
	// auto-load would silently load nothing).
	for _, phase := range mode.DefaultPhases() {
		for _, name := range mode.PhaseSkills(phase) {
			if !got[name] {
				t.Errorf("phase %q declares skill %q but the pack does not ship it", phase, name)
			}
		}
	}
}

// TestPackSkillsDemandGrounding asserts each bundled skill enforces the evidence
// discipline (D-CODE-8): its body must demonstrate at least one grounded finding
// (one IsGrounded accepts) and explicitly reject generic advice. This is the
// AS-074 acceptance that findings carry a file/symbol/span reference, checked at
// the source the model is told to follow.
func TestPackSkillsDemandGrounding(t *testing.T) {
	pack, err := Pack()
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}
	for _, s := range pack {
		grounded := false
		for _, line := range strings.Split(s.Body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "- ") && IsGrounded(line) {
				grounded = true
				break
			}
		}
		if !grounded {
			t.Errorf("skill %q has no grounded example finding; it must cite a file/symbol/span", s.Name)
		}
		if !strings.Contains(strings.ToLower(s.Body), "grounding") {
			t.Errorf("skill %q does not state the grounding requirement", s.Name)
		}
	}
}

// TestIsGrounded pins the evidence predicate: concrete references pass, generic
// advice fails. It is the machine-checkable form of D-CODE-8 the runtime and the
// pack tests rely on.
func TestIsGrounded(t *testing.T) {
	grounded := []string{
		"internal/session/store.go Load() drops a truncated log",
		"the `Config.Window` field is zero by default",
		"this regresses AS-042 routing defaults",
		"break at internal/mode/mode_test.go:45",
		"calling mode.NextPhase() shifts the result",
	}
	for _, s := range grounded {
		if !IsGrounded(s) {
			t.Errorf("IsGrounded(%q) = false, want true", s)
		}
	}

	generic := []string{
		"Consider following best practices.",
		"Make sure to handle errors properly.",
		"Be careful not to break existing behaviour.",
		"This went well overall.",
	}
	for _, s := range generic {
		if IsGrounded(s) {
			t.Errorf("IsGrounded(%q) = true, want false (generic advice)", s)
		}
	}
}
