package domain

import (
	"fmt"
	"time"
)

// YearMonth identifies a calendar month. It is the unit the grid navigates by,
// and navigation is explicitly unbounded in both directions.
type YearMonth struct {
	Year  int
	Month time.Month
}

// NewYearMonth builds a YearMonth.
func NewYearMonth(year int, month time.Month) YearMonth {
	return YearMonth{Year: year, Month: month}
}

// CurrentYearMonth is the month containing today, the grid's initial view.
func CurrentYearMonth() YearMonth { return Today().YearMonth() }

// ParseYearMonth reads the URL form, YYYY-MM.
func ParseYearMonth(s string) (YearMonth, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return YearMonth{}, fmt.Errorf("parse month %q: expected YYYY-MM", s)
	}
	return YearMonth{Year: t.Year(), Month: t.Month()}, nil
}

// String is the URL and storage form, YYYY-MM.
func (m YearMonth) String() string { return fmt.Sprintf("%04d-%02d", m.Year, int(m.Month)) }

// First is the first calendar day of the month.
func (m YearMonth) First() Date { return Date{Year: m.Year, Month: m.Month, Day: 1} }

// Last is the final calendar day of the month, accounting for leap years.
func (m YearMonth) Last() Date {
	// Day 0 of the following month is the last day of this one; time.Date
	// normalises the year rollover for December on its own.
	t := time.Date(m.Year, m.Month+1, 0, 0, 0, 0, 0, time.UTC)
	return DateOf(t)
}

// Len is the number of days in the month.
func (m YearMonth) Len() int { return m.Last().Day }

// Days lists every calendar day in the month, in order.
func (m YearMonth) Days() []Date {
	days := make([]Date, 0, m.Len())
	for d := 1; d <= m.Len(); d++ {
		days = append(days, Date{Year: m.Year, Month: m.Month, Day: d})
	}
	return days
}

// Next and Prev step by one month. Adding to time.Month past December and
// below January is handled by time.Date's normalisation, so year boundaries
// need no special case here.
func (m YearMonth) Next() YearMonth { return m.AddMonths(1) }
func (m YearMonth) Prev() YearMonth { return m.AddMonths(-1) }

// AddMonths steps by n months in either direction, without bound.
func (m YearMonth) AddMonths(n int) YearMonth {
	t := time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	return YearMonth{Year: t.Year(), Month: t.Month()}
}

// Contains reports whether d falls in this month.
func (m YearMonth) Contains(d Date) bool { return d.Year == m.Year && d.Month == m.Month }

// Months lists every month of a calendar year, for the yearly report.
func Months(year int) []YearMonth {
	out := make([]YearMonth, 0, 12)
	for mo := time.January; mo <= time.December; mo++ {
		out = append(out, YearMonth{Year: year, Month: mo})
	}
	return out
}
