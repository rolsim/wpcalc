package domain

import (
	"fmt"
	"time"
)

// Date is a timezone-free calendar date.
//
// The grid is a calendar artifact: "2026-07-14" names the same day regardless
// of where the server runs or which offset the browser is in. Carrying
// time.Time here would invite exactly the off-by-one-day bugs that corrupt
// month boundaries and totals, so the domain refuses to model an instant.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate builds a Date. It does not normalise: callers pass real calendar days.
func NewDate(year int, month time.Month, day int) Date {
	return Date{Year: year, Month: month, Day: day}
}

// DateOf reduces a time.Time to the calendar date it falls on in its own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// Today is the current calendar date in the machine's local zone.
func Today() Date { return DateOf(time.Now()) }

// ParseDate reads the ISO form, YYYY-MM-DD. This is the storage and URL form;
// Display is the human one.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("parse date %q: expected YYYY-MM-DD", s)
	}
	return DateOf(t), nil
}

// Time renders the date as midnight UTC, for the few places that need a
// time.Time (weekday arithmetic, day stepping). UTC is arbitrary but fixed,
// which is the point: no DST transition can shift a calendar day.
func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// String is the ISO form used in storage, URLs, and form values.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// Display is the Swiss form, DD.MM.YYYY.
func (d Date) Display() string {
	return fmt.Sprintf("%02d.%02d.%04d", d.Day, int(d.Month), d.Year)
}

// IsZero reports whether the date is the unset zero value.
func (d Date) IsZero() bool { return d == Date{} }

// Compare orders two dates: negative if d is earlier, zero if equal.
func (d Date) Compare(o Date) int {
	if d.Year != o.Year {
		return d.Year - o.Year
	}
	if d.Month != o.Month {
		return int(d.Month) - int(o.Month)
	}
	return d.Day - o.Day
}

func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }
func (d Date) After(o Date) bool  { return d.Compare(o) > 0 }
func (d Date) Equal(o Date) bool  { return d.Compare(o) == 0 }

// AddDays steps by whole days, crossing month and year boundaries correctly.
func (d Date) AddDays(n int) Date { return DateOf(d.Time().AddDate(0, 0, n)) }

// Weekday reports the day of the week.
func (d Date) Weekday() time.Weekday { return d.Time().Weekday() }

// IsWeekend reports Saturday or Sunday, which the grid shades.
func (d Date) IsWeekend() bool {
	w := d.Weekday()
	return w == time.Saturday || w == time.Sunday
}

// YearMonth returns the month this date falls in.
func (d Date) YearMonth() YearMonth { return YearMonth{Year: d.Year, Month: d.Month} }
