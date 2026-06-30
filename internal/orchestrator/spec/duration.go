package spec

import (
	"fmt"
	"time"
)

// Duration is a job-spec duration. The DSL grammar is `^[0-9]+(s|m|h|d)$` with
// `d` = 24h (docs/design/job-spec-dsl.md §4.4) — deliberately *wider* than
// time.ParseDuration (which has no `d`) and *narrower* (no compound `1h30m`, no
// fractions, no bare ints). We therefore parse it ourselves rather than reach
// for time.ParseDuration so `90d` is legal and `1h30m` is not.
type Duration struct {
	d time.Duration
}

// Std returns the duration as a time.Duration.
func (d Duration) Std() time.Duration { return d.d }

func (d Duration) String() string { return d.d.String() }

// ParseDuration parses one duration token under the DSL grammar. Anything
// outside `^[0-9]+(s|m|h|d)$` — a bare int, a compound value, a fractional unit,
// a negative, or an empty string — is rejected (rule 16).
func ParseDuration(s string) (Duration, error) {
	if s == "" {
		return Duration{}, fmt.Errorf("empty duration")
	}
	// Split into a digit run and exactly one trailing unit byte.
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i != len(s)-1 {
		// No leading digits, or more than one trailing character (compound /
		// fractional / bare-int values all land here).
		return Duration{}, fmt.Errorf("duration %q must match ^[0-9]+(s|m|h|d)$", s)
	}
	// Parse the integer manually; it is already known to be all digits.
	var n int64
	for _, c := range s[:i] {
		n = n*10 + int64(c-'0')
	}
	var unit time.Duration
	switch s[i] {
	case 's':
		unit = time.Second
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	default:
		return Duration{}, fmt.Errorf("duration %q has unknown unit %q; want s|m|h|d", s, string(s[i]))
	}
	return Duration{d: time.Duration(n) * unit}, nil
}
