package domain

import (
	"testing"
	"time"
)

func TestMonthNavigationCrossesYearBoundary(t *testing.T) {
	cases := []struct {
		from       YearMonth
		next, prev YearMonth
	}{
		{NewYearMonth(2026, time.December), NewYearMonth(2027, time.January), NewYearMonth(2026, time.November)},
		{NewYearMonth(2026, time.January), NewYearMonth(2026, time.February), NewYearMonth(2025, time.December)},
		{NewYearMonth(2000, time.January), NewYearMonth(2000, time.February), NewYearMonth(1999, time.December)},
	}
	for _, c := range cases {
		if got := c.from.Next(); got != c.next {
			t.Errorf("%s.Next() = %s, want %s", c.from, got, c.next)
		}
		if got := c.from.Prev(); got != c.prev {
			t.Errorf("%s.Prev() = %s, want %s", c.from, got, c.prev)
		}
	}
}

func TestMonthNavigationIsUnbounded(t *testing.T) {
	// Navigation is explicitly indefinite in both directions; stepping a
	// century forward and back must return exactly where it started.
	start := NewYearMonth(2026, time.July)
	m := start
	for i := 0; i < 1200; i++ {
		m = m.Next()
	}
	if want := NewYearMonth(2126, time.July); m != want {
		t.Fatalf("after 1200 forward steps: %s, want %s", m, want)
	}
	for i := 0; i < 1200; i++ {
		m = m.Prev()
	}
	if m != start {
		t.Fatalf("after returning: %s, want %s", m, start)
	}
}

func TestMonthLengthHandlesLeapYears(t *testing.T) {
	cases := []struct {
		m    YearMonth
		want int
	}{
		{NewYearMonth(2026, time.February), 28},
		{NewYearMonth(2024, time.February), 29}, // divisible by 4
		{NewYearMonth(2000, time.February), 29}, // divisible by 400
		{NewYearMonth(1900, time.February), 28}, // divisible by 100 but not 400
		{NewYearMonth(2026, time.January), 31},
		{NewYearMonth(2026, time.April), 30},
		{NewYearMonth(2026, time.December), 31},
	}
	for _, c := range cases {
		if got := c.m.Len(); got != c.want {
			t.Errorf("%s.Len() = %d, want %d", c.m, got, c.want)
		}
		if got := len(c.m.Days()); got != c.want {
			t.Errorf("len(%s.Days()) = %d, want %d", c.m, got, c.want)
		}
		if last := c.m.Last(); last.Day != c.want || !c.m.Contains(last) {
			t.Errorf("%s.Last() = %s, want day %d inside the month", c.m, last, c.want)
		}
	}
}

func TestParseYearMonthRoundTrip(t *testing.T) {
	for _, s := range []string{"2026-07", "1999-12", "2027-01"} {
		m, err := ParseYearMonth(s)
		if err != nil {
			t.Errorf("ParseYearMonth(%q): %v", s, err)
			continue
		}
		if got := m.String(); got != s {
			t.Errorf("round trip %q -> %q", s, got)
		}
	}
	for _, s := range []string{"2026-13", "2026", "26-07", "", "2026-00", "not-a-month"} {
		if _, err := ParseYearMonth(s); err == nil {
			t.Errorf("ParseYearMonth(%q) succeeded, want error", s)
		}
	}
}

func TestWeekendDetection(t *testing.T) {
	// 2026-07-04 is a Saturday, 2026-07-05 a Sunday.
	cases := map[string]bool{
		"2026-07-03": false, // Fri
		"2026-07-04": true,  // Sat
		"2026-07-05": true,  // Sun
		"2026-07-06": false, // Mon
	}
	for s, want := range cases {
		d, err := ParseDate(s)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", s, err)
		}
		if got := d.IsWeekend(); got != want {
			t.Errorf("%s (%s).IsWeekend() = %v, want %v", s, d.Weekday(), got, want)
		}
	}
}

func TestDateAddDaysCrossesBoundaries(t *testing.T) {
	cases := []struct {
		from string
		n    int
		want string
	}{
		{"2026-07-31", 1, "2026-08-01"},
		{"2026-12-31", 1, "2027-01-01"},
		{"2026-01-01", -1, "2025-12-31"},
		{"2024-02-28", 1, "2024-02-29"}, // leap
		{"2026-02-28", 1, "2026-03-01"}, // non-leap
	}
	for _, c := range cases {
		d, err := ParseDate(c.from)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", c.from, err)
		}
		if got := d.AddDays(c.n).String(); got != c.want {
			t.Errorf("%s.AddDays(%d) = %s, want %s", c.from, c.n, got, c.want)
		}
	}
}

func TestDateDisplayIsSwissFormat(t *testing.T) {
	d := NewDate(2026, time.July, 4)
	if got, want := d.Display(), "04.07.2026"; got != want {
		t.Errorf("Display() = %q, want %q", got, want)
	}
	if got, want := d.String(), "2026-07-04"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestParseDateRejectsMalformed(t *testing.T) {
	for _, s := range []string{"04.07.2026", "2026-7-4", "2026-02-30", "", "yesterday"} {
		if _, err := ParseDate(s); err == nil {
			t.Errorf("ParseDate(%q) succeeded, want error", s)
		}
	}
}
