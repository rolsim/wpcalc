package main

import (
	"context"
	"flag"
	"fmt"

	"errors"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
	"uuid"
)

// cmdSampleEmployees creates placeholder employment records so the grid has
// columns to render.
//
// It deliberately records no hours and no day comments. This is a timesheet:
// fabricated entries are indistinguishable from real ones once they are in the
// database, and a demo database that has been copied, inherited, or pointed at
// by mistake would then contain invented records of work people did not do.
// The employment periods alone are enough to show the grid, the weekend
// shading, the visibility rule, and the locked cells — and every cell starts
// empty, which is the honest starting state anyway.
//
// The names are obvious placeholders for the same reason.
func cmdSampleEmployees(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sample-employees", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	monthFlag := fs.String("month", domain.CurrentYearMonth().String(),
		"month the employment periods are arranged around, YYYY-MM")
	tenantFlag := fs.String("tenant", "",
		"tenant id the placeholder employees belong to (default: the Default tenant every fresh database starts with)")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}

	month, err := domain.ParseYearMonth(*monthFlag)
	if err != nil {
		return err
	}

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// An explicit -tenant wins; otherwise fall back to the Default tenant by
	// name. There is no longer a well-known id to default to — that was the
	// point of moving to UUIDs — so the seeded tenant is found the way a
	// person would find it.
	tenantID, err := resolveSeedTenant(ctx, db, *tenantFlag)
	if err != nil {
		return err
	}

	// A deliberate mix, so the rules that are easy to get wrong are visible at
	// a glance: two employed across the whole month, one joining midway (half
	// the row locked), and one who left the month before (absent entirely).
	people := []struct{ name, start, end string }{
		{"Muster A", month.First().String(), ""},
		{"Muster B", month.First().String(), ""},
		{"Muster C", month.First().AddDays(14).String(), ""},
		{"Muster D", month.Prev().First().String(), month.Prev().Last().String()},
	}

	created := 0
	for _, p := range people {
		e := domain.Employee{TenantID: tenantID, DisplayName: p.name}
		if e.StartDate, err = domain.ParseDate(p.start); err != nil {
			return err
		}
		if p.end != "" {
			d, err := domain.ParseDate(p.end)
			if err != nil {
				return err
			}
			e.EndDate = &d
		}
		if _, err := db.CreateEmployee(ctx, e); err != nil {
			return err
		}
		created++
	}

	fmt.Printf("created %d placeholder employees around %s in %s (no hours recorded)\n",
		created, month, db.Path())
	return nil
}

// resolveSeedTenant turns the -tenant flag into an id: parsed when given,
// otherwise the tenant named "Default" that a fresh database starts with.
func resolveSeedTenant(ctx context.Context, db *store.DB, flagValue string) (uuid.UUID, error) {
	if flagValue != "" {
		id, err := uuid.Parse(flagValue)
		if err != nil {
			return uuid.Nil(), fmt.Errorf("sample-employees: -tenant %q is not a uuid: %w", flagValue, err)
		}
		return id, nil
	}
	tenants, err := db.Tenants(ctx)
	if err != nil {
		return uuid.Nil(), err
	}
	for _, t := range tenants {
		if t.Name == "Default" {
			return t.ID, nil
		}
	}
	return uuid.Nil(), errors.New(`sample-employees: no tenant named "Default"; pass -tenant with a tenant id`)
}
