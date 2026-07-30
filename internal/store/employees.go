package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rolsim/wpcalc/internal/domain"
)

// ErrNotFound is returned when a lookup by id matches no row.
var ErrNotFound = errors.New("not found")

// CreateEmployee inserts an employee and returns its id.
func (db *DB) CreateEmployee(ctx context.Context, e domain.Employee) (int64, error) {
	if err := e.Validate(); err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO employees (tenant_id, display_name, start_date, end_date) VALUES (?, ?, ?, ?)`,
		e.TenantID, e.DisplayName, e.StartDate.String(), nullDate(e.EndDate))
	if err != nil {
		if isForeignKeyViolation(err) {
			return 0, fmt.Errorf("store: create employee: tenant %d: %w", e.TenantID, ErrNotFound)
		}
		return 0, fmt.Errorf("store: create employee: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: create employee: %w", err)
	}
	return id, nil
}

// UpdateEmployee saves name and employment dates. The employee's tenant is
// not touched here — moving an employee between tenants is not something
// this method does.
//
// Shortening an employment can strand entries outside the new interval. Those
// rows are left in place on purpose: deleting recorded hours because someone
// fixed a typo in a date would destroy data the grid never warned about. They
// stop being editable and stop appearing; the fix is to widen the dates back.
func (db *DB) UpdateEmployee(ctx context.Context, e domain.Employee) error {
	if err := e.Validate(); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx,
		`UPDATE employees
		    SET display_name = ?, start_date = ?, end_date = ?, updated_at = datetime('now')
		  WHERE id = ?`,
		e.DisplayName, e.StartDate.String(), nullDate(e.EndDate), e.ID)
	if err != nil {
		return fmt.Errorf("store: update employee %d: %w", e.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update employee %d: %w", e.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update employee %d: %w", e.ID, ErrNotFound)
	}
	return nil
}

// DeleteEmployee removes an employee and, by cascade, their entries.
func (db *DB) DeleteEmployee(ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM employees WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete employee %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete employee %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete employee %d: %w", id, ErrNotFound)
	}
	return nil
}

// Employee fetches one employee by id — global, not tenant-scoped, since an
// id is unique across every tenant. Callers that must not leak across
// tenants (any HTTP route) check the returned TenantID themselves.
func (db *DB) Employee(ctx context.Context, id int64) (domain.Employee, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, tenant_id, display_name, start_date, end_date FROM employees WHERE id = ?`, id)
	e, err := scanEmployee(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Employee{}, fmt.Errorf("store: employee %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return domain.Employee{}, fmt.Errorf("store: employee %d: %w", id, err)
	}
	return e, nil
}

// Employees lists every employee in a tenant, ordered for stable display.
func (db *DB) Employees(ctx context.Context, tenantID int64) ([]domain.Employee, error) {
	return db.queryEmployees(ctx,
		`SELECT id, tenant_id, display_name, start_date, end_date
		   FROM employees
		  WHERE tenant_id = ?
		  ORDER BY display_name COLLATE NOCASE, id`,
		tenantID)
}

// EmployeesActiveIn lists only those in a tenant whose employment overlaps
// the month.
//
// The overlap is computed in SQL rather than by filtering in Go so that a
// month with two active people out of two hundred former ones reads two rows.
// It mirrors domain.Employee.ActiveIn exactly, and a test pins them together.
func (db *DB) EmployeesActiveIn(ctx context.Context, tenantID int64, m domain.YearMonth) ([]domain.Employee, error) {
	return db.queryEmployees(ctx,
		`SELECT id, tenant_id, display_name, start_date, end_date
		   FROM employees
		  WHERE tenant_id = ?
		    AND start_date <= ?
		    AND (end_date IS NULL OR end_date >= ?)
		  ORDER BY display_name COLLATE NOCASE, id`,
		tenantID, m.Last().String(), m.First().String())
}

func (db *DB) queryEmployees(ctx context.Context, query string, args ...any) ([]domain.Employee, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list employees: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Employee
	for rows.Next() {
		e, err := scanEmployee(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list employees: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list employees: %w", err)
	}
	return out, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanEmployee(s scanner) (domain.Employee, error) {
	var (
		e       domain.Employee
		start   string
		end     sql.NullString
		parsErr error
	)
	if err := s.Scan(&e.ID, &e.TenantID, &e.DisplayName, &start, &end); err != nil {
		return domain.Employee{}, err
	}
	if e.StartDate, parsErr = domain.ParseDate(start); parsErr != nil {
		return domain.Employee{}, parsErr
	}
	if end.Valid {
		d, err := domain.ParseDate(end.String)
		if err != nil {
			return domain.Employee{}, err
		}
		e.EndDate = &d
	}
	return e, nil
}

func nullDate(d *domain.Date) any {
	if d == nil {
		return nil
	}
	return d.String()
}
