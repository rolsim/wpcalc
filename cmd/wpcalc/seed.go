package main

import (
	"context"
	"flag"
	"fmt"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

// cmdDemoSeed fills a database with a plausible month so the grid, the totals,
// and all three PDFs can be looked at without an hour of typing first.
//
// It is deterministic: the same database always comes out the same way, so a
// screenshot or a PDF from it can be compared against a later one.
func cmdDemoSeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("demo-seed", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	monthFlag := fs.String("month", domain.CurrentYearMonth().String(), "month to fill, YYYY-MM")
	if err := fs.Parse(args); err != nil {
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

	// A deliberate mix: two full-month people, one who joins mid-month, and
	// one who left the month before. The last two are what make the locked
	// cells and the visibility rule visible at a glance.
	people := []struct {
		name       string
		start, end string
		pattern    []domain.Centihours
	}{
		{"Anna Muster", month.First().String(), "", []domain.Centihours{800, 800, 800, 800, 750}},
		{"Jürg Müller", month.First().String(), "", []domain.Centihours{850, 800, 775, 800, 600}},
		{"Sofia Rossi", month.First().AddDays(14).String(), "", []domain.Centihours{400, 400, 800, 800, 425}},
		{"Peter Vergangen", month.Prev().First().String(), month.Prev().Last().String(), nil},
	}

	for _, p := range people {
		e := domain.Employee{DisplayName: p.name}
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

		id, err := db.CreateEmployee(ctx, e)
		if err != nil {
			return err
		}
		if p.pattern == nil {
			continue
		}

		// Weekdays only, cycling the pattern — a timesheet with hours booked
		// on every Sunday would not look like anything real.
		i := 0
		for _, day := range month.Days() {
			if day.IsWeekend() || !e.Employed(day) {
				continue
			}
			if err := db.SetHours(ctx, id, day, p.pattern[i%len(p.pattern)]); err != nil {
				return err
			}
			i++
		}
	}

	comments := map[int]string{
		1:  "Monatsbeginn",
		14: "Betriebsausflug",
		24: "Kundentermin Zürich",
	}
	for day, text := range comments {
		if day > month.Len() {
			continue
		}
		d := domain.NewDate(month.Year, month.Month, day)
		if err := db.SetDayComment(ctx, d, text); err != nil {
			return err
		}
	}

	totals, err := db.Totals(ctx, month)
	if err != nil {
		return err
	}
	fmt.Printf("seeded %s in %s: %d employees, %s total\n",
		month, db.Path(), len(people), totals.Grand.Format("."))
	return nil
}
