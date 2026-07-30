package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rolsim/wpcalc/internal/domain"
)

// ErrDuplicateTenant is returned when a tenant name is already taken.
var ErrDuplicateTenant = errors.New("tenant name already exists")

// CreateTenant adds a tenant ("Mandant") and returns its id.
func (db *DB) CreateTenant(ctx context.Context, name string) (int64, error) {
	if err := domain.ValidTenantName(name); err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `INSERT INTO tenants (name) VALUES (?)`, name)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("store: create tenant %q: %w", name, ErrDuplicateTenant)
		}
		return 0, fmt.Errorf("store: create tenant: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: create tenant: %w", err)
	}
	return id, nil
}

// RenameTenant changes a tenant's name.
func (db *DB) RenameTenant(ctx context.Context, id int64, name string) error {
	if err := domain.ValidTenantName(name); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE tenants SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("store: rename tenant %d: %w", id, ErrDuplicateTenant)
		}
		return fmt.Errorf("store: rename tenant %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rename tenant %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: rename tenant %d: %w", id, ErrNotFound)
	}
	return nil
}

// Tenant fetches one tenant by id.
func (db *DB) Tenant(ctx context.Context, id int64) (domain.Tenant, error) {
	var t domain.Tenant
	err := db.QueryRowContext(ctx, `SELECT id, name FROM tenants WHERE id = ?`, id).Scan(&t.ID, &t.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Tenant{}, fmt.Errorf("store: tenant %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("store: tenant %d: %w", id, err)
	}
	return t, nil
}

// Tenants lists every tenant, ordered for stable display.
func (db *DB) Tenants(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM tenants ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("store: list tenants: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list tenants: %w", err)
	}
	return out, nil
}
