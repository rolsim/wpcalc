// Package store is the SQLite persistence layer. It owns schema migration and
// every query; nothing above it writes SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"

	"github.com/pressly/goose/v3"

	// Pure-Go SQLite. Deliberately not mattn/go-sqlite3, which needs cgo and
	// would cost us the static binary the WordPress shim has to spawn.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB is a migrated database handle.
type DB struct {
	*sql.DB
	path string
}

// Open opens (creating if absent) the database at path and brings the schema
// up to date. The brief calls for the file to be created on demand, so a
// missing file is the normal first-run case, not an error.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store: database path is required")
	}

	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	// SQLite tolerates exactly one writer. Serialising here turns what would
	// be intermittent "database is locked" failures under concurrent cell
	// edits into ordinary queueing.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: connect %s: %w", path, err)
	}

	db := &DB{DB: sqlDB, path: path}
	if err := db.Migrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// dsn builds the connection string. The pragmas matter:
//
//   - foreign_keys is OFF by default in SQLite, which would silently orphan
//     time_entries when an employee is deleted despite the declared cascade.
//   - busy_timeout turns lock contention into a wait rather than an error.
//   - WAL lets the reporting reads proceed while a cell edit is committing.
func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	return "file:" + path + "?" + q.Encode()
}

// Path is the file this database lives in.
func (db *DB) Path() string { return db.path }

// Migrate applies any pending migrations.
//
// It uses goose's Provider rather than the package-level API: the latter keeps
// dialect and filesystem in process globals, which race as soon as two tests
// migrate two databases at once.
func (db *DB) Migrate(ctx context.Context) error {
	p, err := newProvider(db.DB)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate up: %w", err)
	}
	return nil
}

// MigrateDown rolls back a single migration. Used by `wpcalc migrate down` and
// by the up/down/up test that proves the Down blocks are real.
func (db *DB) MigrateDown(ctx context.Context) error {
	p, err := newProvider(db.DB)
	if err != nil {
		return err
	}
	if _, err := p.Down(ctx); err != nil {
		return fmt.Errorf("store: migrate down: %w", err)
	}
	return nil
}

// MigrationStatus reports each migration and whether it has been applied.
func (db *DB) MigrationStatus(ctx context.Context) ([]string, error) {
	p, err := newProvider(db.DB)
	if err != nil {
		return nil, err
	}
	st, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: migration status: %w", err)
	}
	out := make([]string, 0, len(st))
	for _, s := range st {
		state := "pending"
		if s.State == goose.StateApplied {
			state = "applied"
		}
		out = append(out, fmt.Sprintf("%-10s %s", state, filepath.Base(s.Source.Path)))
	}
	return out, nil
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: locate migrations: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sub,
		// Migrations are embedded and registered per-provider; the global
		// registry would leak state between databases in the same process.
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("store: init migrations: %w", err)
	}
	return p, nil
}
