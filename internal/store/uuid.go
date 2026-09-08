package store

import (
	"database/sql"
	"fmt"
	"uuid"
)

// The standard library's uuid.UUID is a [16]byte with text marshalling but no
// database/sql support — it implements neither driver.Valuer nor sql.Scanner,
// so it can be neither passed as a query argument nor scanned out of a row on
// its own. These four adapters are that missing layer, and the only place in
// the codebase that knows how a UUID is spelled in SQLite.
//
// Ids are stored as canonical 36-character text rather than a 16-byte BLOB.
// Text costs 20 bytes more per id, which is irrelevant at the scale of one
// company's timesheet, and buys back a database that is legible in a sqlite3
// shell and in a dump — a BLOB id turns every manual query into hex juggling.

// arg renders a UUID as a query argument.
func arg(u uuid.UUID) string { return u.String() }

// nullArg renders an optional UUID, preserving SQL NULL for the nil pointer.
// user_roles.tenant_id and .employee_id are genuinely nullable: which of them
// is set is what makes an assignment system-, tenant-, or employee-scoped.
func nullArg(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

// scan reads a non-null id column into dst.
type scan struct{ dst *uuid.UUID }

func (s scan) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("store: uuid column is %T, want string", src)
	}
	u, err := uuid.Parse(text)
	if err != nil {
		return fmt.Errorf("store: parse uuid %q: %w", text, err)
	}
	*s.dst = u
	return nil
}

// scanNull reads a nullable id column, leaving dst nil on SQL NULL.
type scanNull struct{ dst **uuid.UUID }

func (s scanNull) Scan(src any) error {
	if src == nil {
		*s.dst = nil
		return nil
	}
	var u uuid.UUID
	if err := (scan{&u}).Scan(src); err != nil {
		return err
	}
	*s.dst = &u
	return nil
}

// sql.Scanner is looked up dynamically by database/sql, so a missing or
// misspelled method would surface as a driver error inside whichever query ran
// first rather than at compile time.
var (
	_ sql.Scanner = scan{}
	_ sql.Scanner = scanNull{}
)
