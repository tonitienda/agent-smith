package orchestrator

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSchedule is a parsed 5-field cron expression (minute hour day-of-month
// month day-of-week), the schedule grammar the job-spec DSL fixes for `cron`
// triggers (docs/design/job-spec-dsl.md §4.2). It is deliberately small: the DSL
// requires an explicit IANA timezone alongside the schedule, so timezone handling
// lives with Next, not in the expression.
type cronSchedule struct {
	min, hour, dom, month, dow   uint64 // bitsets of allowed values
	domRestricted, dowRestricted bool
}

type cronField struct {
	name     string
	min, max int
}

var cronFields = []cronField{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 6}, // 0 = Sunday
}

// parseCron parses a 5-field cron expression. Anything outside the supported
// grammar (`*`, `*/step`, `a-b`, `a,b`, and single values, per field) is an error
// — fail-closed, like the rest of the spec format.
func parseCron(expr string) (cronSchedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cronSchedule{}, fmt.Errorf("cron %q must have 5 fields, got %d", expr, len(parts))
	}
	var s cronSchedule
	sets := make([]uint64, 5)
	for i, p := range parts {
		bits, star, err := parseCronField(p, cronFields[i])
		if err != nil {
			return cronSchedule{}, fmt.Errorf("cron %q: %s: %w", expr, cronFields[i].name, err)
		}
		sets[i] = bits
		if i == 2 && !star {
			s.domRestricted = true
		}
		if i == 4 && !star {
			s.dowRestricted = true
		}
	}
	s.min, s.hour, s.dom, s.month, s.dow = sets[0], sets[1], sets[2], sets[3], sets[4]
	return s, nil
}

func parseCronField(field string, f cronField) (bits uint64, star bool, err error) {
	if field == "*" {
		for v := f.min; v <= f.max; v++ {
			bits |= 1 << uint(v)
		}
		return bits, true, nil
	}
	for _, part := range strings.Split(field, ",") {
		step := 1
		rng := part
		if slash := strings.IndexByte(part, '/'); slash >= 0 {
			rng = part[:slash]
			step, err = strconv.Atoi(part[slash+1:])
			if err != nil || step < 1 {
				return 0, false, fmt.Errorf("bad step %q", part)
			}
		}
		lo, hi := f.min, f.max
		if rng != "*" {
			if dash := strings.IndexByte(rng, '-'); dash >= 0 {
				lo, err = strconv.Atoi(rng[:dash])
				if err != nil {
					return 0, false, fmt.Errorf("bad range %q", part)
				}
				hi, err = strconv.Atoi(rng[dash+1:])
				if err != nil {
					return 0, false, fmt.Errorf("bad range %q", part)
				}
			} else {
				lo, err = strconv.Atoi(rng)
				if err != nil {
					return 0, false, fmt.Errorf("bad value %q", part)
				}
				hi = lo
			}
		}
		if lo < f.min || hi > f.max || lo > hi {
			return 0, false, fmt.Errorf("value out of range %q (want %d-%d)", part, f.min, f.max)
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, false, nil
}

// next returns the first time at or after `after` (exclusive of `after` itself)
// that matches the schedule in location loc. It searches minute-by-minute and
// gives up after ~4 years, which only an unsatisfiable schedule (e.g. Feb 30)
// reaches.
func (s cronSchedule) next(after time.Time, loc *time.Location) (time.Time, bool) {
	t := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(4, 0, 0)
	// Advance step-wise: when the month/day/hour does not match, jump to the start
	// of the next candidate unit rather than crawling minute by minute. This keeps
	// even an unsatisfiable schedule (e.g. Feb 30) bounded by months/days, not the
	// ~2M minutes a full minute-by-minute scan of the 4-year window would take.
	for t.Before(limit) {
		if s.month&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if s.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
			continue
		}
		if s.min&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

// dayMatches applies the standard cron day-field quirk: when both day-of-month and
// day-of-week are restricted, a match on either fires; otherwise the unrestricted
// field is a wildcard and both must hold.
func (s cronSchedule) dayMatches(t time.Time) bool {
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(int(t.Weekday()))) != 0
	if s.domRestricted && s.dowRestricted {
		return domOK || dowOK
	}
	return domOK && dowOK
}
