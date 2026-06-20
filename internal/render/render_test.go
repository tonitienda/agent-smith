package render_test

import (
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/tonitienda/agent-smith/internal/render"
)

func TestTokens(t *testing.T) {
	cases := map[int]string{
		-3_000_000: "-3M tok",
		-1_234:     "-1.2k tok",
		-42:        "-42 tok",
		0:          "0 tok",
		42:         "42 tok",
		999:        "999 tok",
		1_000:      "1k tok",
		1_234:      "1.2k tok",
		12_000:     "12k tok",
		2_500_000:  "2.5M tok",
		3_000_000:  "3M tok",
	}
	for n, want := range cases {
		if got := render.Tokens(n); got != want {
			t.Errorf("Tokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	cases := []struct {
		n    int
		noun string
		want string
	}{
		{0, "segment", "0 segments"},
		{1, "segment", "1 segment"},
		{2, "block", "2 blocks"},
	}
	for _, c := range cases {
		if got := render.Count(c.n, c.noun); got != c.want {
			t.Errorf("Count(%d, %q) = %q, want %q", c.n, c.noun, got, c.want)
		}
	}
}

func TestCommas(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		999:     "999",
		1_000:   "1,000",
		12_000:  "12,000",
		1234567: "1,234,567",
		-1234:   "-1,234",
	}
	for n, want := range cases {
		if got := render.Commas(n); got != want {
			t.Errorf("Commas(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestMoney(t *testing.T) {
	if got := render.Money("$", 1.5); got != "$1.5000" {
		t.Errorf("Money($, 1.5) = %q, want %q", got, "$1.5000")
	}
	if got := render.Money("$", -1.5); got != "-$1.5000" {
		t.Errorf("Money($, -1.5) = %q, want %q", got, "-$1.5000")
	}
	if got := render.Money("EUR ", 0); got != "EUR 0.0000" {
		t.Errorf("Money(EUR , 0) = %q, want %q", got, "EUR 0.0000")
	}
}

func TestTimestamp(t *testing.T) {
	ts := time.Date(2026, 6, 20, 9, 4, 30, 0, time.UTC)
	if got := render.Timestamp(ts); got != "2026-06-20 09:04" {
		t.Errorf("Timestamp = %q, want %q", got, "2026-06-20 09:04")
	}
}

func TestTab(t *testing.T) {
	var b strings.Builder
	tw, row := render.Tab(&b, tabwriter.AlignRight)
	row("a\tbb\t\n")
	row("ccc\td\t\n")
	if err := tw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := "    a  bb\n  ccc   d\n"
	if b.String() != want {
		t.Errorf("Tab output = %q, want %q", b.String(), want)
	}
}
