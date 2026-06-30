package orchestrator

import (
	"testing"
	"time"
)

func mustNext(t *testing.T, expr string, after time.Time) (time.Time, bool) {
	t.Helper()
	s, err := parseCron(expr)
	if err != nil {
		t.Fatalf("parseCron(%q): %v", expr, err)
	}
	return s.next(after, time.UTC)
}

func TestCronNextDaily(t *testing.T) {
	got, ok := mustNext(t, "0 3 * * *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	want := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("next = %v ok=%v, want %v", got, ok, want)
	}
}

func TestCronNextSpecificDayMonth(t *testing.T) {
	// 09:30 on the 15th of March.
	got, ok := mustNext(t, "30 9 15 3 *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	want := time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("next = %v ok=%v, want %v", got, ok, want)
	}
}

func TestCronUnsatisfiableReturnsFalse(t *testing.T) {
	// February 30th never occurs: the step-wise search must terminate, not spin.
	if _, ok := mustNext(t, "0 0 30 2 *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("Feb 30 should be unsatisfiable")
	}
}

func TestCronDomDowOrQuirk(t *testing.T) {
	// Both day fields restricted → match on EITHER the 13th OR any Friday.
	// 2026-02-06 is a Friday and not the 13th, so it must match before the 13th.
	got, ok := mustNext(t, "0 0 13 * 5", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("expected a match")
	}
	if got.Weekday() != time.Friday && got.Day() != 13 {
		t.Fatalf("next = %v, want a Friday or the 13th", got)
	}
	want := time.Date(2026, 2, 6, 0, 0, 0, 0, time.UTC) // first Friday
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v (first Friday)", got, want)
	}
}
