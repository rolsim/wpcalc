package domain

import (
	"errors"
	"testing"
)

func TestParseHours_CommaAndDot(t *testing.T) {
	// A de-CH keyboard produces a comma and a numpad produces a dot; both are
	// the same number and must parse identically.
	pairs := []struct {
		dot, comma string
		want       Centihours
	}{
		{"7.75", "7,75", 775},
		{"0.25", "0,25", 25},
		{"8.0", "8,0", 800},
		{"24.00", "24,00", 2400},
	}
	for _, p := range pairs {
		gotDot, err := ParseHours(p.dot)
		if err != nil {
			t.Errorf("ParseHours(%q) unexpected error: %v", p.dot, err)
			continue
		}
		gotComma, err := ParseHours(p.comma)
		if err != nil {
			t.Errorf("ParseHours(%q) unexpected error: %v", p.comma, err)
			continue
		}
		if gotDot != p.want || gotComma != p.want {
			t.Errorf("ParseHours(%q)=%d, ParseHours(%q)=%d, want %d",
				p.dot, gotDot, p.comma, gotComma, p.want)
		}
	}
}

func TestParseHours_Accepts(t *testing.T) {
	cases := map[string]Centihours{
		"":       0, // cleared cell
		"   ":    0,
		"0":      0,
		"7":      700,
		"7.5":    750, // seven and a half, not seven-and-five-hundredths
		"7.05":   705,
		"7.":     700,
		".5":     50,
		" 7.75 ": 775,
		"24":     2400,
		"23.99":  2399,
	}
	for in, want := range cases {
		got, err := ParseHours(in)
		if err != nil {
			t.Errorf("ParseHours(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseHours(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseHours_RejectsGarbage(t *testing.T) {
	// Every one of these must be refused rather than coerced: a silently
	// altered value in a timesheet is worse than a visible error.
	bad := []string{
		"abc",
		"7h30",
		"7:45",  // plausible, but not the format this field takes
		"7.755", // three decimals: rounding would change a submitted figure
		"1.2.3",
		".",
		"-1",
		"-0.5",
		"+7",
		"1e3",
		"0x10",
		"7 5",
		"24.01", // over a single day
		"99",
		"1000000000000",
	}
	for _, in := range bad {
		if got, err := ParseHours(in); err == nil {
			t.Errorf("ParseHours(%q) = %d, want error", in, got)
		}
	}
}

func TestParseHours_RangeErrorsAreIdentifiable(t *testing.T) {
	// Handlers distinguish "out of range" from "malformed" to phrase the
	// field error; both must stay matchable with errors.Is.
	if _, err := ParseHours("24.01"); !errors.Is(err, ErrHoursRange) {
		t.Errorf("over-cap value: got %v, want ErrHoursRange", err)
	}
	if _, err := ParseHours("-1"); !errors.Is(err, ErrHoursRange) {
		t.Errorf("negative value: got %v, want ErrHoursRange", err)
	}
}

func TestCentihoursFormat(t *testing.T) {
	cases := []struct {
		in        Centihours
		sep, want string
	}{
		{0, ".", "0.00"},
		{5, ".", "0.05"},
		{50, ".", "0.50"},
		{700, ".", "7.00"},
		{775, ".", "7.75"},
		{775, ",", "7,75"},
		{2400, ".", "24.00"},
		{19325, ".", "193.25"}, // a month's total
	}
	for _, c := range cases {
		if got := c.in.Format(c.sep); got != c.want {
			t.Errorf("Centihours(%d).Format(%q) = %q, want %q", c.in, c.sep, got, c.want)
		}
	}
}

func TestCentihoursRoundTrip(t *testing.T) {
	// Anything we render must parse back to itself, or editing a cell would
	// drift its value every time someone opened and saved it.
	for v := Centihours(0); v <= MaxDailyCentihours; v++ {
		got, err := ParseHours(v.String())
		if err != nil {
			t.Fatalf("ParseHours(%q) error: %v", v.String(), err)
		}
		if got != v {
			t.Fatalf("round trip of %d via %q gave %d", v, v.String(), got)
		}
	}
}

func TestCentihoursSumIsExact(t *testing.T) {
	// The whole reason this type is an integer. A month of 0.10 h entries
	// summed as float64 does not land on a round number; as Centihours it must.
	var total Centihours
	for i := 0; i < 30; i++ {
		total += 10 // 0.10 h
	}
	if total != 300 {
		t.Fatalf("30 x 0.10 h = %s, want 3.00", total)
	}
	if got := total.Format("."); got != "3.00" {
		t.Fatalf("formatted %q, want \"3.00\"", got)
	}
}
