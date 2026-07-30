package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigration4PreservesExistingEmployeesAndHours is the scenario the unit
// tests around Open (always a brand-new, empty database) never exercised:
// migrating a database that already has employees — and, critically, time
// entries pointing at them — up through 00004_rbac.sql.
//
// SQLite refuses to add a REFERENCES column with a non-NULL default to a
// table that already has rows once foreign_keys is on ("Cannot add a
// REFERENCES column with non-NULL default value"), and this repo runs with
// foreign_keys on. Every existing automated test migrated an empty
// database, so this went unnoticed until it was run against a real one.
// This test fails loudly if that regresses — and, just as importantly, if a
// future rewrite of that migration cascade-deletes time_entries instead
// (SQLite propagates a foreign key's ON DELETE CASCADE through a RENAME,
// so a rebuild-the-table fix for the same restriction is its own hazard).
func TestMigration4PreservesExistingEmployeesAndHours(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "upgrade.db")

	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)

	p, err := newProvider(sqlDB)
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}

	// Land on the pre-RBAC schema (migrations 1-3), exactly what a real
	// deployment already at that version looks like.
	if _, err := p.UpTo(ctx, 3); err != nil {
		t.Fatalf("UpTo(3): %v", err)
	}

	res, err := sqlDB.ExecContext(ctx,
		`INSERT INTO employees (display_name, start_date) VALUES ('Anna', '2026-01-01')`)
	if err != nil {
		t.Fatalf("insert employee: %v", err)
	}
	empID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO time_entries (employee_id, work_date, centihours) VALUES (?, '2026-07-14', 775)`, empID); err != nil {
		t.Fatalf("insert time entry: %v", err)
	}

	// The migration under test.
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("Up (migrating through 00004): %v", err)
	}

	var tenantID int64
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT tenant_id FROM employees WHERE id = ?`, empID).Scan(&tenantID); err != nil {
		t.Fatalf("read back tenant_id: %v", err)
	}
	if tenantID != 1 {
		t.Errorf("employee's tenant_id = %d, want 1 (backfilled to the Default tenant)", tenantID)
	}

	var hours int64
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT centihours FROM time_entries WHERE employee_id = ?`, empID).Scan(&hours); err != nil {
		t.Fatalf("recorded hours did not survive the migration: %v", err)
	}
	if hours != 775 {
		t.Errorf("centihours = %d, want 775", hours)
	}

	var violations int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_foreign_key_check()`).Scan(&violations); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	if violations != 0 {
		t.Errorf("foreign_key_check reports %d violation(s) after migrating", violations)
	}
}
