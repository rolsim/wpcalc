package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/rolsim/wpcalc/internal/domain"
)

// SetHours records an employee's hours for one day.
//
// Zero clears the cell by deleting the row rather than storing 0. An absent
// row and a recorded zero would render identically but mean different things,
// and keeping only one of them makes "did anyone touch this cell" answerable.
//
// The employment interval is enforced here, not only in the handler. The
// template greys locked cells and the handler checks before writing, but this
// is the layer that cannot be bypassed by a crafted request, a future second
// caller, or the seed command.
func (db *DB) SetHours(ctx context.Context, employeeID int64, d domain.Date, h domain.Centihours) error {
	emp, err := db.Employee(ctx, employeeID)
	if err != nil {
		return err
	}
	if !emp.Employed(d) {
		return fmt.Errorf("store: %s on %s: %w", emp.DisplayName, d.Display(), domain.ErrNotEmployed)
	}
	if h < 0 || h > domain.MaxDailyCentihours {
		return fmt.Errorf("store: %s on %s: %w", emp.DisplayName, d.Display(), domain.ErrHoursRange)
	}

	if h == 0 {
		_, err := db.ExecContext(ctx,
			`DELETE FROM time_entries WHERE employee_id = ? AND work_date = ?`,
			employeeID, d.String())
		if err != nil {
			return fmt.Errorf("store: clear hours: %w", err)
		}
		return nil
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO time_entries (employee_id, work_date, centihours)
		      VALUES (?, ?, ?)
		 ON CONFLICT (employee_id, work_date)
		   DO UPDATE SET centihours = excluded.centihours, updated_at = datetime('now')`,
		employeeID, d.String(), int64(h))
	if err != nil {
		return fmt.Errorf("store: set hours: %w", err)
	}
	return nil
}

// Hours reads a single cell. A cleared cell reads as zero, not as an error.
func (db *DB) Hours(ctx context.Context, employeeID int64, d domain.Date) (domain.Centihours, error) {
	var v int64
	err := db.QueryRowContext(ctx,
		`SELECT centihours FROM time_entries WHERE employee_id = ? AND work_date = ?`,
		employeeID, d.String()).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: hours: %w", err)
	}
	return domain.Centihours(v), nil
}

// MonthEntries returns every recorded cell in the month for one tenant, keyed
// by employee and then by day, so the grid renders from one query instead of
// one per cell.
//
// time_entries carries no tenant_id of its own (see migration 00004's
// comment on why: employees.tenant_id is the one source of truth, joined
// here rather than duplicated) — leaving this join out would leak every
// tenant's hours into every other tenant's grid.
func (db *DB) MonthEntries(ctx context.Context, tenantID int64, m domain.YearMonth) (map[int64]map[domain.Date]domain.Centihours, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT te.employee_id, te.work_date, te.centihours
		   FROM time_entries te
		   JOIN employees e ON e.id = te.employee_id
		  WHERE e.tenant_id = ? AND te.work_date BETWEEN ? AND ?`,
		tenantID, m.First().String(), m.Last().String())
	if err != nil {
		return nil, fmt.Errorf("store: month entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[int64]map[domain.Date]domain.Centihours)
	for rows.Next() {
		var (
			empID int64
			day   string
			v     int64
		)
		if err := rows.Scan(&empID, &day, &v); err != nil {
			return nil, fmt.Errorf("store: month entries: %w", err)
		}
		d, err := domain.ParseDate(day)
		if err != nil {
			return nil, fmt.Errorf("store: month entries: %w", err)
		}
		if out[empID] == nil {
			out[empID] = make(map[domain.Date]domain.Centihours)
		}
		out[empID][d] = domain.Centihours(v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: month entries: %w", err)
	}
	return out, nil
}

// MonthTotals holds the grid's accumulators.
//
// All three are computed by SQL over the same rows in one pass each, rather
// than summed in the template. The grand total is derived from the per-day
// figures so that it cannot disagree with the axis it is printed against.
type MonthTotals struct {
	PerEmployee map[int64]domain.Centihours
	PerDay      map[domain.Date]domain.Centihours
	Grand       domain.Centihours
}

// Totals computes both accumulators for a month, scoped to one tenant.
func (db *DB) Totals(ctx context.Context, tenantID int64, m domain.YearMonth) (MonthTotals, error) {
	t := MonthTotals{
		PerEmployee: make(map[int64]domain.Centihours),
		PerDay:      make(map[domain.Date]domain.Centihours),
	}

	empRows, err := db.QueryContext(ctx,
		`SELECT te.employee_id, SUM(te.centihours)
		   FROM time_entries te
		   JOIN employees e ON e.id = te.employee_id
		  WHERE e.tenant_id = ? AND te.work_date BETWEEN ? AND ?
		  GROUP BY te.employee_id`,
		tenantID, m.First().String(), m.Last().String())
	if err != nil {
		return t, fmt.Errorf("store: totals per employee: %w", err)
	}
	defer func() { _ = empRows.Close() }()
	for empRows.Next() {
		var id, sum int64
		if err := empRows.Scan(&id, &sum); err != nil {
			return t, fmt.Errorf("store: totals per employee: %w", err)
		}
		t.PerEmployee[id] = domain.Centihours(sum)
	}
	if err := empRows.Err(); err != nil {
		return t, fmt.Errorf("store: totals per employee: %w", err)
	}

	dayRows, err := db.QueryContext(ctx,
		`SELECT te.work_date, SUM(te.centihours)
		   FROM time_entries te
		   JOIN employees e ON e.id = te.employee_id
		  WHERE e.tenant_id = ? AND te.work_date BETWEEN ? AND ?
		  GROUP BY te.work_date`,
		tenantID, m.First().String(), m.Last().String())
	if err != nil {
		return t, fmt.Errorf("store: totals per day: %w", err)
	}
	defer func() { _ = dayRows.Close() }()
	for dayRows.Next() {
		var (
			day string
			sum int64
		)
		if err := dayRows.Scan(&day, &sum); err != nil {
			return t, fmt.Errorf("store: totals per day: %w", err)
		}
		d, err := domain.ParseDate(day)
		if err != nil {
			return t, fmt.Errorf("store: totals per day: %w", err)
		}
		t.PerDay[d] = domain.Centihours(sum)
		t.Grand += domain.Centihours(sum)
	}
	if err := dayRows.Err(); err != nil {
		return t, fmt.Errorf("store: totals per day: %w", err)
	}

	return t, nil
}

// EmployeeRangeTotal sums one employee's hours over an inclusive date range.
// The reports use it for both the monthly and the yearly figures.
func (db *DB) EmployeeRangeTotal(ctx context.Context, employeeID int64, from, to domain.Date) (domain.Centihours, error) {
	var sum sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT SUM(centihours)
		   FROM time_entries
		  WHERE employee_id = ? AND work_date BETWEEN ? AND ?`,
		employeeID, from.String(), to.String()).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("store: employee range total: %w", err)
	}
	if !sum.Valid {
		return 0, nil
	}
	return domain.Centihours(sum.Int64), nil
}

// EmployeeEntries lists one employee's recorded days in a range, in date order.
func (db *DB) EmployeeEntries(ctx context.Context, employeeID int64, from, to domain.Date) ([]domain.TimeEntry, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT work_date, centihours
		   FROM time_entries
		  WHERE employee_id = ? AND work_date BETWEEN ? AND ?
		  ORDER BY work_date`,
		employeeID, from.String(), to.String())
	if err != nil {
		return nil, fmt.Errorf("store: employee entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.TimeEntry
	for rows.Next() {
		var (
			day string
			v   int64
		)
		if err := rows.Scan(&day, &v); err != nil {
			return nil, fmt.Errorf("store: employee entries: %w", err)
		}
		d, err := domain.ParseDate(day)
		if err != nil {
			return nil, fmt.Errorf("store: employee entries: %w", err)
		}
		out = append(out, domain.TimeEntry{EmployeeID: employeeID, Date: d, Hours: domain.Centihours(v)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: employee entries: %w", err)
	}
	return out, nil
}

// SetDayComment stores the single note for a calendar day within a tenant. An
// empty or whitespace-only comment removes it, mirroring how clearing an
// hours cell works.
func (db *DB) SetDayComment(ctx context.Context, tenantID int64, d domain.Date, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		if _, err := db.ExecContext(ctx,
			`DELETE FROM day_comments WHERE tenant_id = ? AND work_date = ?`, tenantID, d.String()); err != nil {
			return fmt.Errorf("store: clear day comment: %w", err)
		}
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO day_comments (tenant_id, work_date, comment)
		      VALUES (?, ?, ?)
		 ON CONFLICT (tenant_id, work_date)
		   DO UPDATE SET comment = excluded.comment, updated_at = datetime('now')`,
		tenantID, d.String(), comment)
	if err != nil {
		return fmt.Errorf("store: set day comment: %w", err)
	}
	return nil
}

// DayComments returns every comment in the month for one tenant, keyed by day.
func (db *DB) DayComments(ctx context.Context, tenantID int64, m domain.YearMonth) (map[domain.Date]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT work_date, comment FROM day_comments WHERE tenant_id = ? AND work_date BETWEEN ? AND ?`,
		tenantID, m.First().String(), m.Last().String())
	if err != nil {
		return nil, fmt.Errorf("store: day comments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[domain.Date]string)
	for rows.Next() {
		var day, comment string
		if err := rows.Scan(&day, &comment); err != nil {
			return nil, fmt.Errorf("store: day comments: %w", err)
		}
		d, err := domain.ParseDate(day)
		if err != nil {
			return nil, fmt.Errorf("store: day comments: %w", err)
		}
		out[d] = comment
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: day comments: %w", err)
	}
	return out, nil
}
