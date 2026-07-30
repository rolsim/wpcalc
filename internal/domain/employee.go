package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Employee is someone whose hours the grid tracks, over a bounded employment
// interval. EndDate nil means still employed.
type Employee struct {
	ID          int64
	TenantID    int64
	DisplayName string
	StartDate   Date
	EndDate     *Date
}

// ErrInvalidEmployee is the sentinel for validation failures.
var ErrInvalidEmployee = errors.New("invalid employee")

// ErrNotEmployed is returned when hours are booked on a day outside someone's
// employment interval. The grid greys those cells, but greying is a hint to
// the browser and not a control: the write path rejects them independently.
var ErrNotEmployed = errors.New("outside employment period")

// Employed reports whether d falls inside the employment interval, both ends
// inclusive: someone who starts on the 14th can book hours on the 14th.
//
// This is the single source of truth for the lock rule. The template greys
// locked cells and the handler rejects writes to them, but both ask this.
func (e Employee) Employed(d Date) bool {
	if d.Before(e.StartDate) {
		return false
	}
	if e.EndDate != nil && d.After(*e.EndDate) {
		return false
	}
	return true
}

// ActiveIn reports whether the employment interval overlaps the month at all.
//
// Employees with no overlap are omitted from the grid entirely rather than
// rendered as a column of locked cells — a leaver from three years ago should
// not widen every month you page through.
func (e Employee) ActiveIn(m YearMonth) bool {
	if e.StartDate.After(m.Last()) {
		return false
	}
	if e.EndDate != nil && e.EndDate.Before(m.First()) {
		return false
	}
	return true
}

// Validate checks the invariants the store and handlers both rely on.
func (e Employee) Validate() error {
	if e.TenantID == 0 {
		return fmt.Errorf("%w: tenant is required", ErrInvalidEmployee)
	}
	if strings.TrimSpace(e.DisplayName) == "" {
		return fmt.Errorf("%w: display name is required", ErrInvalidEmployee)
	}
	if e.StartDate.IsZero() {
		return fmt.Errorf("%w: start date is required", ErrInvalidEmployee)
	}
	if e.EndDate != nil && e.EndDate.Before(e.StartDate) {
		return fmt.Errorf("%w: end date %s is before start date %s",
			ErrInvalidEmployee, e.EndDate.Display(), e.StartDate.Display())
	}
	return nil
}

// ActiveEmployees filters to those overlapping the month, preserving order.
func ActiveEmployees(all []Employee, m YearMonth) []Employee {
	out := make([]Employee, 0, len(all))
	for _, e := range all {
		if e.ActiveIn(m) {
			out = append(out, e)
		}
	}
	return out
}

// TimeEntry is one employee's hours on one day. The store holds at most one
// row per (employee, day); a zero value means the cell was cleared.
type TimeEntry struct {
	EmployeeID int64
	Date       Date
	Hours      Centihours
}

// DayComment is the single free-text note attached to a calendar day. It
// belongs to the day, not to any employee.
type DayComment struct {
	Date    Date
	Comment string
}
