package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Centihours is a working duration in hundredths of an hour — "industrial
// minutes", where 7.75 h is 7 h 45 min and stores as 775.
//
// This is an integer on purpose. The grid sums the same entries along two axes
// and the PDFs sum them a third time; those three totals must agree to the
// last digit. Binary floats cannot promise that: 0.1+0.2 already disagrees
// with 0.3, and the error compounds across a month of entries. Integers make
// the reconciliation exact by construction rather than by rounding at display.
type Centihours int64

// MaxDailyCentihours caps a single cell at 24.00 h. A day cannot hold more,
// and a larger value is far more likely a typo than a real entry.
const MaxDailyCentihours Centihours = 2400

// ErrHoursRange is returned when a parsed value is negative or exceeds a day.
var ErrHoursRange = errors.New("hours out of range")

// ParseHours reads a decimal hours figure.
//
// It accepts both separators, because a de-CH keyboard produces "7,75" and a
// numpad produces "7.75", and rejecting either would be a papercut on every
// single cell. Empty input means "clear this cell" and yields 0.
//
// Everything else is rejected rather than coerced: silently reading "7.755" as
// 7.75, or "7h30" as 7, would put a wrong number in a timesheet without ever
// telling the person who typed it.
func ParseHours(s string) (Centihours, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	norm := strings.ReplaceAll(s, ",", ".")
	if strings.HasPrefix(norm, "-") {
		return 0, fmt.Errorf("%w: %q is negative", ErrHoursRange, s)
	}

	whole, frac, hasSep := strings.Cut(norm, ".")
	if strings.Contains(frac, ".") {
		return 0, fmt.Errorf("invalid hours %q: more than one decimal separator", s)
	}
	if hasSep && whole == "" && frac == "" {
		return 0, fmt.Errorf("invalid hours %q: no digits", s)
	}
	if len(frac) > 2 {
		// Rounding here would quietly alter a submitted timesheet value.
		return 0, fmt.Errorf("invalid hours %q: at most two decimal places", s)
	}

	hours, err := parseDigits(whole)
	if err != nil {
		return 0, fmt.Errorf("invalid hours %q: %w", s, err)
	}

	// "7.5" is seven and a half hours, not seven and five hundredths.
	frac += strings.Repeat("0", 2-len(frac))
	cents, err := parseDigits(frac)
	if err != nil {
		return 0, fmt.Errorf("invalid hours %q: %w", s, err)
	}

	total := Centihours(hours*100 + cents)
	if total > MaxDailyCentihours {
		return 0, fmt.Errorf("%w: %s exceeds 24.00 h in one day", ErrHoursRange, s)
	}
	return total, nil
}

// parseDigits accepts only ASCII digits, so "1e3", "0x10", "+7", and unicode
// digits are all refused. An empty string is zero, which lets "7." and ".5"
// through as 7.00 and 0.50.
func parseDigits(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%q is not a number", s)
		}
		n = n*10 + int64(r-'0')
		if n > 1_000_000 {
			return 0, errors.New("value too large")
		}
	}
	return n, nil
}

// Format renders decimal hours with a fixed two places, using sep as the
// decimal separator so the locale decides between "7.75" and "7,75".
func (c Centihours) Format(sep string) string {
	neg := ""
	v := c
	if v < 0 {
		neg, v = "-", -v
	}
	return fmt.Sprintf("%s%d%s%02d", neg, v/100, sep, v%100)
}

// String uses the ISO-ish dot form, for logs, tests, and form round-tripping.
func (c Centihours) String() string { return c.Format(".") }

// Hours is the float view, for the rare consumer that needs one. Do not sum
// these: sum Centihours and convert once at the end.
func (c Centihours) Hours() float64 { return float64(c) / 100 }

// IsZero reports an empty cell.
func (c Centihours) IsZero() bool { return c == 0 }
