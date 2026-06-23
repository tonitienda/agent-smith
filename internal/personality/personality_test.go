package personality

import "testing"

func ptr(b bool) *bool { return &b }

func TestSeriousModeMutesEverything(t *testing.T) {
	// AC1: toggling serious mode removes all references with zero behavioral effect.
	p := New(Settings{Theme: "matrix", SeriousMode: ptr(false)}, true)
	if got := p.Name(RoleUser); got != "Mr. Anderson" {
		t.Fatalf("themed user name = %q, want Mr. Anderson", got)
	}
	if got := p.StatusLine(); got == plainStatusLine {
		t.Fatalf("themed status line = %q, want a themed line", got)
	}

	if newState := p.ToggleSerious(); !newState {
		t.Fatalf("ToggleSerious returned %v, want true", newState)
	}
	if !p.Serious() {
		t.Fatal("Serious() = false after toggle, want true")
	}
	if got := p.Name(RoleUser); got != plainNames[RoleUser] {
		t.Fatalf("serious user name = %q, want plain %q", got, plainNames[RoleUser])
	}
	if got := p.StatusLine(); got != plainStatusLine {
		t.Fatalf("serious status line = %q, want %q", got, plainStatusLine)
	}

	// Toggling back restores the theme — the switch is total and reversible.
	if p.ToggleSerious() {
		t.Fatal("ToggleSerious returned true on second toggle, want false")
	}
	if got := p.Name(RoleRouter); got != "The Keymaker" {
		t.Fatalf("restored router name = %q, want The Keymaker", got)
	}
}

func TestNonInteractiveDefaultsSerious(t *testing.T) {
	// AC3: non-interactive faces default to clean output. With serious_mode unset,
	// a non-interactive face resolves to serious; the TUI resolves to themed.
	headless := New(Settings{Theme: "matrix"}, false)
	if !headless.Serious() {
		t.Fatal("non-interactive face: Serious() = false, want true by default")
	}
	if got := headless.StatusLine(); got != plainStatusLine {
		t.Fatalf("non-interactive status line = %q, want plain", got)
	}

	tui := New(Settings{Theme: "matrix"}, true)
	if tui.Serious() {
		t.Fatal("interactive face: Serious() = true, want false by default")
	}
}

func TestExplicitSeriousOverridesFaceDefault(t *testing.T) {
	// An explicit serious_mode wins over the face default, both directions.
	if !New(Settings{SeriousMode: ptr(true)}, true).Serious() {
		t.Fatal("explicit serious_mode:true in TUI should be serious")
	}
	if New(Settings{SeriousMode: ptr(false)}, false).Serious() {
		t.Fatal("explicit serious_mode:false headless should not be serious")
	}
}

func TestNameMapOverridesAndIntensity(t *testing.T) {
	// AC4: name-map overrides and intensity work per Appendix D.
	p := New(Settings{
		Theme:       "matrix",
		Names:       map[string]string{"user": "Neo", "router": "  "},
		SeriousMode: ptr(false),
	}, true)
	if got := p.Name(RoleUser); got != "Neo" {
		t.Fatalf("overridden user name = %q, want Neo", got)
	}
	// A blank override is ignored, falling back to the built-in themed name.
	if got := p.Name(RoleRouter); got != "The Keymaker" {
		t.Fatalf("blank override should fall back: got %q", got)
	}

	// Subtle intensity confines flavor to status/loading — no renaming.
	subtle := New(Settings{Theme: "matrix", Intensity: "subtle", SeriousMode: ptr(false)}, true)
	if got := subtle.Name(RoleUser); got != plainNames[RoleUser] {
		t.Fatalf("subtle intensity should not rename: got %q", got)
	}
	if got := subtle.StatusLine(); got == plainStatusLine {
		t.Fatalf("subtle intensity should still theme the status line: got %q", got)
	}
}

func TestIntensityResolution(t *testing.T) {
	// AS-126: default is medium (rain + renaming on), "full" is a medium alias,
	// and unknown values fall back to medium rather than failing.
	cases := []struct {
		in   string
		want Intensity
	}{
		{"", IntensityMedium},
		{"full", IntensityMedium}, // legacy AS-053 alias
		{"medium", IntensityMedium},
		{" Medium ", IntensityMedium},
		{"subtle", IntensitySubtle},
		{"bold", IntensityBold},
		{"nonsense", IntensityMedium},
	}
	for _, c := range cases {
		p := New(Settings{Theme: "matrix", Intensity: c.in, SeriousMode: ptr(false)}, true)
		if got := p.Intensity(); got != c.want {
			t.Errorf("Intensity(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIntensityMutedWhenSeriousOrThemeOff(t *testing.T) {
	// Serious mode and theme "none" must drive the effective intensity to subtle so
	// the rain/phrase chrome goes quiet even if bold was configured (AS-126).
	serious := New(Settings{Theme: "matrix", Intensity: "bold", SeriousMode: ptr(true)}, true)
	if got := serious.Intensity(); got != IntensitySubtle {
		t.Fatalf("serious Intensity() = %d, want subtle", got)
	}
	none := New(Settings{Theme: "none", Intensity: "bold", SeriousMode: ptr(false)}, true)
	if got := none.Intensity(); got != IntensitySubtle {
		t.Fatalf("theme-none Intensity() = %d, want subtle", got)
	}
	// Toggling serious back on a medium config restores the configured intensity.
	p := New(Settings{Theme: "matrix", SeriousMode: ptr(false)}, true)
	if p.Intensity() != IntensityMedium {
		t.Fatalf("default Intensity() = %d, want medium", p.Intensity())
	}
	p.SetSerious(true)
	if p.Intensity() != IntensitySubtle {
		t.Fatalf("after SetSerious(true) Intensity() = %d, want subtle", p.Intensity())
	}
	p.SetSerious(false)
	if p.Intensity() != IntensityMedium {
		t.Fatalf("after SetSerious(false) Intensity() = %d, want medium restored", p.Intensity())
	}
}

func TestThemeNoneIsPlain(t *testing.T) {
	p := New(Settings{Theme: "none", SeriousMode: ptr(false)}, true)
	if got := p.Name(RoleUser); got != plainNames[RoleUser] {
		t.Fatalf("theme none should be plain: got %q", got)
	}
	if got := p.StatusLine(); got != plainStatusLine {
		t.Fatalf("theme none status line = %q, want plain", got)
	}
}

func TestUnknownRoleFallsBack(t *testing.T) {
	p := New(Settings{Theme: "matrix", SeriousMode: ptr(false)}, true)
	if got := p.Name(Role("nonesuch")); got != "nonesuch" {
		t.Fatalf("unknown role = %q, want raw fallback", got)
	}
}
