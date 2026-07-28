package domain

import (
	"testing"
	"time"
)

func date(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func ptrDate(t *testing.T, s string) *Date {
	t.Helper()
	d := date(t, s)
	return &d
}

func TestEmployeeHiddenWhenNoOverlapWithMonth(t *testing.T) {
	july := NewYearMonth(2026, time.July)

	cases := []struct {
		name  string
		emp   Employee
		shown bool
	}{
		{
			name:  "left long before, still employed",
			emp:   Employee{DisplayName: "Leaver", StartDate: date(t, "2020-01-01"), EndDate: ptrDate(t, "2020-06-30")},
			shown: false,
		},
		{
			name:  "starts after the month ends",
			emp:   Employee{DisplayName: "Future", StartDate: date(t, "2026-08-01")},
			shown: false,
		},
		{
			name:  "ends the day before the month starts",
			emp:   Employee{DisplayName: "JustMissed", StartDate: date(t, "2025-01-01"), EndDate: ptrDate(t, "2026-06-30")},
			shown: false,
		},
		{
			name:  "ends on the first day of the month",
			emp:   Employee{DisplayName: "OneDay", StartDate: date(t, "2025-01-01"), EndDate: ptrDate(t, "2026-07-01")},
			shown: true,
		},
		{
			name:  "starts on the last day of the month",
			emp:   Employee{DisplayName: "LateJoiner", StartDate: date(t, "2026-07-31")},
			shown: true,
		},
		{
			name:  "open ended, started years ago",
			emp:   Employee{DisplayName: "Regular", StartDate: date(t, "2020-01-01")},
			shown: true,
		},
		{
			name:  "employed for part of the month only",
			emp:   Employee{DisplayName: "Partial", StartDate: date(t, "2026-07-10"), EndDate: ptrDate(t, "2026-07-20")},
			shown: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.emp.ActiveIn(july); got != c.shown {
				t.Errorf("ActiveIn(%s) = %v, want %v", july, got, c.shown)
			}
		})
	}
}

func TestActiveEmployeesFiltersAndPreservesOrder(t *testing.T) {
	july := NewYearMonth(2026, time.July)
	all := []Employee{
		{ID: 1, DisplayName: "A", StartDate: date(t, "2020-01-01")},
		{ID: 2, DisplayName: "B", StartDate: date(t, "2020-01-01"), EndDate: ptrDate(t, "2021-01-01")},
		{ID: 3, DisplayName: "C", StartDate: date(t, "2026-07-15")},
		{ID: 4, DisplayName: "D", StartDate: date(t, "2030-01-01")},
	}
	got := ActiveEmployees(all, july)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("got %d employees %v, want IDs [1 3]", len(got), got)
	}
}

func TestEmploymentIntervalIsInclusiveAtBothEnds(t *testing.T) {
	// Someone starting on the 14th books hours on the 14th; someone leaving on
	// the 20th books hours on the 20th. Off-by-one here silently loses a day
	// of someone's pay.
	e := Employee{
		DisplayName: "Boundary",
		StartDate:   date(t, "2026-07-14"),
		EndDate:     ptrDate(t, "2026-07-20"),
	}
	cases := map[string]bool{
		"2026-07-13": false,
		"2026-07-14": true,
		"2026-07-17": true,
		"2026-07-20": true,
		"2026-07-21": false,
	}
	for s, want := range cases {
		if got := e.Employed(date(t, s)); got != want {
			t.Errorf("Employed(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestOpenEndedEmploymentHasNoUpperBound(t *testing.T) {
	e := Employee{DisplayName: "Open", StartDate: date(t, "2026-07-14")}
	for _, s := range []string{"2026-07-14", "2027-01-01", "2099-12-31"} {
		if !e.Employed(date(t, s)) {
			t.Errorf("Employed(%s) = false, want true for open-ended employment", s)
		}
	}
	if e.Employed(date(t, "2026-07-13")) {
		t.Error("Employed before start date = true, want false")
	}
}

func TestEmployeeValidate(t *testing.T) {
	valid := Employee{DisplayName: "Fine", StartDate: date(t, "2026-01-01")}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid employee rejected: %v", err)
	}

	bad := []struct {
		name string
		emp  Employee
	}{
		{"empty name", Employee{DisplayName: "", StartDate: date(t, "2026-01-01")}},
		{"blank name", Employee{DisplayName: "   ", StartDate: date(t, "2026-01-01")}},
		{"no start date", Employee{DisplayName: "NoStart"}},
		{"end before start", Employee{
			DisplayName: "Backwards",
			StartDate:   date(t, "2026-07-01"),
			EndDate:     ptrDate(t, "2026-06-01"),
		}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if err := c.emp.Validate(); err == nil {
				t.Error("Validate() = nil, want error")
			}
		})
	}
}

func TestEndDateEqualToStartDateIsOneDayOfEmployment(t *testing.T) {
	d := date(t, "2026-07-14")
	e := Employee{DisplayName: "SingleDay", StartDate: d, EndDate: &d}
	if err := e.Validate(); err != nil {
		t.Fatalf("single-day employment rejected: %v", err)
	}
	if !e.Employed(d) {
		t.Error("Employed on the only day of employment = false, want true")
	}
	if e.Employed(d.AddDays(1)) || e.Employed(d.AddDays(-1)) {
		t.Error("employed outside the single day")
	}
}
