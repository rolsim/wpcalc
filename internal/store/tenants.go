package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"uuid"

	"github.com/rolsim/wpcalc/internal/domain"
)

// ErrDuplicateTenant is returned when a tenant name is already taken.
var ErrDuplicateTenant = errors.New("tenant name already exists")

// CreateTenant adds a tenant ("Mandant") and returns its id.
//
// The id is minted here rather than by the database. That is the necessary
// consequence of dropping AUTOINCREMENT: there is no sequence left to ask, so
// LastInsertId has nothing to return, and the caller learns the id because we
// chose it — not because the row landed at some position.
func (db *DB) CreateTenant(ctx context.Context, name string) (uuid.UUID, error) {
	if err := domain.ValidTenantName(name); err != nil {
		return uuid.Nil(), err
	}
	id := uuid.NewV4()
	_, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name) VALUES (?, ?)`, arg(id), name)
	if err != nil {
		if isUniqueViolation(err) {
			return uuid.Nil(), fmt.Errorf("store: create tenant %q: %w", name, ErrDuplicateTenant)
		}
		return uuid.Nil(), fmt.Errorf("store: create tenant: %w", err)
	}
	return id, nil
}

// RenameTenant changes a tenant's name.
func (db *DB) RenameTenant(ctx context.Context, id uuid.UUID, name string) error {
	if err := domain.ValidTenantName(name); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE tenants SET name = ? WHERE id = ?`, name, arg(id))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("store: rename tenant %s: %w", id, ErrDuplicateTenant)
		}
		return fmt.Errorf("store: rename tenant %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rename tenant %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: rename tenant %s: %w", id, ErrNotFound)
	}
	return nil
}

// Tenant fetches one tenant by id.
func (db *DB) Tenant(ctx context.Context, id uuid.UUID) (domain.Tenant, error) {
	var t domain.Tenant
	err := db.QueryRowContext(ctx,
		`SELECT id, name FROM tenants WHERE id = ?`, arg(id)).Scan(scan{&t.ID}, &t.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Tenant{}, fmt.Errorf("store: tenant %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("store: tenant %s: %w", id, err)
	}
	return t, nil
}

// Tenants lists every tenant, ordered for stable display.
//
// The secondary sort is on name, not on id: a UUID orders arbitrarily, so
// tie-breaking on it would scramble equal names between calls rather than
// pinning them the way the old sequential id did.
func (db *DB) Tenants(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM tenants ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		if err := rows.Scan(scan{&t.ID}, &t.Name); err != nil {
			return nil, fmt.Errorf("store: list tenants: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list tenants: %w", err)
	}
	return out, nil
}
