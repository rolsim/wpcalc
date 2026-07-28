package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
)

// testDB opens a real database in a temp file rather than :memory:.
//
// The in-memory driver would exercise a different code path for file creation,
// WAL, and busy handling — the three things most likely to break in the field —
// so the tests pay the disk cost to run against what production runs against.
func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "wpcalc.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func mustEmployee(t *testing.T, db *DB, name, start, end string) int64 {
	t.Helper()
	e := domain.Employee{DisplayName: name, StartDate: mustDate(t, start)}
	if end != "" {
		d := mustDate(t, end)
		e.EndDate = &d
	}
	id, err := db.CreateEmployee(t.Context(), e)
	if err != nil {
		t.Fatalf("CreateEmployee(%s): %v", name, err)
	}
	return id
}

func TestOpenCreatesDatabaseFile(t *testing.T) {
	// The brief requires the database to be created on demand; a missing file
	// is the normal first run, not an error.
	path := filepath.Join(t.TempDir(), "nested", "wpcalc.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist", path)
	}

	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open on missing file: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not created: %v", err)
	}
	if _, err := db.Employees(t.Context()); err != nil {
		t.Errorf("schema not usable after create: %v", err)
	}
}

func TestMigrationsUpDownUpIsClean(t *testing.T) {
	// A Down block that does not actually reverse its Up is worse than no Down
	// block: it fails halfway and leaves a schema nobody can reason about.
	// Open() has already migrated up, so this drives all the way down and back.
	//
	// Every migration is rolled back, not just the newest. A Down block that
	// has never been executed is not known to work, and the one most likely to
	// be wrong is the oldest — the one nobody has run since writing it.
	db := testDB(t)
	ctx := t.Context()

	id := mustEmployee(t, db, "Before", "2026-01-01", "")
	if err := db.SetHours(ctx, id, mustDate(t, "2026-07-14"), 775); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := db.CreateUser(ctx, "someone", "a-long-enough-password", domain.RoleAdmin); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if err := db.MigrateReset(ctx); err != nil {
		t.Fatalf("migrate reset: %v", err)
	}
	if _, err := db.Employees(ctx); err == nil {
		t.Error("employees table still queryable after down migration")
	}
	if _, err := db.Users(ctx); err == nil {
		t.Error("users table still queryable after down migration")
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}

	// The schema must be usable, and empty — down really dropped the data.
	emps, err := db.Employees(ctx)
	if err != nil {
		t.Fatalf("employees after re-up: %v", err)
	}
	if len(emps) != 0 {
		t.Errorf("got %d employees after down/up, want 0", len(emps))
	}
	newID := mustEmployee(t, db, "After", "2026-01-01", "")
	if err := db.SetHours(ctx, newID, mustDate(t, "2026-07-14"), 800); err != nil {
		t.Errorf("schema not writable after re-up: %v", err)
	}
}

func TestMigrationStatusReportsApplied(t *testing.T) {
	st, err := testDB(t).MigrationStatus(t.Context())
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	if len(st) == 0 {
		t.Fatal("no migrations reported")
	}
	for _, line := range st {
		if !strings.Contains(line, "applied") {
			t.Errorf("migration not applied after Open: %q", line)
		}
	}
}

func TestEntryRejectedBeforeStartDate(t *testing.T) {
	db := testDB(t)
	id := mustEmployee(t, db, "Joiner", "2026-07-14", "")

	err := db.SetHours(t.Context(), id, mustDate(t, "2026-07-13"), 800)
	if !errors.Is(err, domain.ErrNotEmployed) {
		t.Fatalf("SetHours the day before start: got %v, want ErrNotEmployed", err)
	}

	// And the boundary day itself must be accepted.
	if err := db.SetHours(t.Context(), id, mustDate(t, "2026-07-14"), 800); err != nil {
		t.Errorf("SetHours on the start date itself: %v", err)
	}
}

func TestEntryRejectedAfterEndDate(t *testing.T) {
	db := testDB(t)
	id := mustEmployee(t, db, "Leaver", "2026-01-01", "2026-07-20")

	err := db.SetHours(t.Context(), id, mustDate(t, "2026-07-21"), 800)
	if !errors.Is(err, domain.ErrNotEmployed) {
		t.Fatalf("SetHours the day after end: got %v, want ErrNotEmployed", err)
	}

	if err := db.SetHours(t.Context(), id, mustDate(t, "2026-07-20"), 800); err != nil {
		t.Errorf("SetHours on the end date itself: %v", err)
	}
}

func TestSetHoursIsIdempotentAndClears(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id := mustEmployee(t, db, "Worker", "2026-01-01", "")
	day := mustDate(t, "2026-07-14")

	if err := db.SetHours(ctx, id, day, 775); err != nil {
		t.Fatalf("SetHours: %v", err)
	}
	if got, _ := db.Hours(ctx, id, day); got != 775 {
		t.Errorf("after first write: %d, want 775", got)
	}

	// Overwriting the same cell updates in place rather than inserting again.
	if err := db.SetHours(ctx, id, day, 800); err != nil {
		t.Fatalf("SetHours overwrite: %v", err)
	}
	if got, _ := db.Hours(ctx, id, day); got != 800 {
		t.Errorf("after overwrite: %d, want 800", got)
	}

	// Zero clears the cell: an absent row, not a stored zero.
	if err := db.SetHours(ctx, id, day, 0); err != nil {
		t.Fatalf("SetHours zero: %v", err)
	}
	entries, err := db.EmployeeEntries(ctx, id, day, day)
	if err != nil {
		t.Fatalf("EmployeeEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("clearing left %d rows, want the row deleted", len(entries))
	}
	if got, _ := db.Hours(ctx, id, day); got != 0 {
		t.Errorf("cleared cell reads %d, want 0", got)
	}
}

func TestSetHoursRejectsOutOfRange(t *testing.T) {
	db := testDB(t)
	id := mustEmployee(t, db, "Worker", "2026-01-01", "")
	day := mustDate(t, "2026-07-14")

	for _, v := range []domain.Centihours{-1, 2401, 10000} {
		if err := db.SetHours(t.Context(), id, day, v); !errors.Is(err, domain.ErrHoursRange) {
			t.Errorf("SetHours(%d): got %v, want ErrHoursRange", v, err)
		}
	}
}

func TestTotalsEqualSumOfEntries(t *testing.T) {
	// The grid prints the same numbers summed two different ways. If the axes
	// can disagree, every figure on the page is suspect.
	db := testDB(t)
	ctx := t.Context()
	july := domain.NewYearMonth(2026, time.July)

	alice := mustEmployee(t, db, "Alice", "2026-01-01", "")
	bob := mustEmployee(t, db, "Bob", "2026-01-01", "")

	written := map[int64]map[domain.Date]domain.Centihours{alice: {}, bob: {}}
	var want domain.Centihours

	// Deliberately awkward values: repeated tenths are exactly what float
	// accumulation gets wrong.
	values := []domain.Centihours{10, 10, 10, 775, 825, 5, 1, 1250}
	for i, day := range july.Days() {
		for j, emp := range []int64{alice, bob} {
			v := values[(i+j)%len(values)]
			if err := db.SetHours(ctx, emp, day, v); err != nil {
				t.Fatalf("SetHours: %v", err)
			}
			written[emp][day] = v
			want += v
		}
	}

	totals, err := db.Totals(ctx, july)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}

	if totals.Grand != want {
		t.Errorf("grand total %s, want %s", totals.Grand, want)
	}

	// Per-employee column totals must sum to the same grand total.
	var byEmployee domain.Centihours
	for emp, cells := range written {
		var expect domain.Centihours
		for _, v := range cells {
			expect += v
		}
		if got := totals.PerEmployee[emp]; got != expect {
			t.Errorf("employee %d total %s, want %s", emp, got, expect)
		}
		byEmployee += totals.PerEmployee[emp]
	}
	if byEmployee != totals.Grand {
		t.Errorf("employee totals sum to %s but grand total is %s", byEmployee, totals.Grand)
	}

	// Per-day row totals must sum to the same grand total.
	var byDay domain.Centihours
	for _, v := range totals.PerDay {
		byDay += v
	}
	if byDay != totals.Grand {
		t.Errorf("day totals sum to %s but grand total is %s", byDay, totals.Grand)
	}
}

func TestTotalsIgnoreEntriesOutsideTheMonth(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id := mustEmployee(t, db, "Worker", "2026-01-01", "")

	for _, d := range []string{"2026-06-30", "2026-07-01", "2026-07-31", "2026-08-01"} {
		if err := db.SetHours(ctx, id, mustDate(t, d), 100); err != nil {
			t.Fatalf("SetHours(%s): %v", d, err)
		}
	}

	totals, err := db.Totals(ctx, domain.NewYearMonth(2026, time.July))
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if totals.Grand != 200 {
		t.Errorf("grand total %s, want 2.00 — boundary days leaked in or out", totals.Grand)
	}
}

func TestEmployeesActiveInMatchesDomainRule(t *testing.T) {
	// The overlap rule exists twice: as SQL in EmployeesActiveIn and as Go in
	// domain.Employee.ActiveIn. Two implementations of one rule drift, and the
	// drift shows up as someone silently missing from a month. Pin them together.
	db := testDB(t)
	ctx := t.Context()

	specs := []struct{ name, start, end string }{
		{"OpenEnded", "2020-01-01", ""},
		{"LeftLongAgo", "2019-01-01", "2019-12-31"},
		{"EndsFirstOfMonth", "2020-01-01", "2026-07-01"},
		{"EndsDayBefore", "2020-01-01", "2026-06-30"},
		{"StartsLastOfMonth", "2026-07-31", ""},
		{"StartsNextMonth", "2026-08-01", ""},
		{"WithinMonth", "2026-07-10", "2026-07-20"},
		{"SpansMonth", "2026-06-01", "2026-08-31"},
	}
	byID := make(map[int64]domain.Employee)
	for _, s := range specs {
		id := mustEmployee(t, db, s.name, s.start, s.end)
		e, err := db.Employee(ctx, id)
		if err != nil {
			t.Fatalf("Employee(%d): %v", id, err)
		}
		byID[id] = e
	}

	// Sweep a range of months, including the boundaries either side.
	for _, m := range []domain.YearMonth{
		domain.NewYearMonth(2026, time.June),
		domain.NewYearMonth(2026, time.July),
		domain.NewYearMonth(2026, time.August),
		domain.NewYearMonth(2019, time.June),
		domain.NewYearMonth(2030, time.January),
	} {
		fromSQL, err := db.EmployeesActiveIn(ctx, m)
		if err != nil {
			t.Fatalf("EmployeesActiveIn(%s): %v", m, err)
		}
		got := make(map[int64]bool, len(fromSQL))
		for _, e := range fromSQL {
			got[e.ID] = true
		}
		for id, e := range byID {
			if want := e.ActiveIn(m); got[id] != want {
				t.Errorf("%s in %s: SQL says %v, domain says %v",
					e.DisplayName, m, got[id], want)
			}
		}
	}
}

func TestDeletingEmployeeCascadesToEntries(t *testing.T) {
	// SQLite leaves foreign keys off unless the connection asks for them, so
	// the declared cascade is only real if the pragma actually took effect.
	db := testDB(t)
	ctx := t.Context()
	id := mustEmployee(t, db, "Temp", "2026-01-01", "")
	day := mustDate(t, "2026-07-14")

	if err := db.SetHours(ctx, id, day, 800); err != nil {
		t.Fatalf("SetHours: %v", err)
	}
	if err := db.DeleteEmployee(ctx, id); err != nil {
		t.Fatalf("DeleteEmployee: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM time_entries WHERE employee_id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if n != 0 {
		t.Errorf("%d orphaned entries survived; foreign_keys pragma is not in effect", n)
	}
}

func TestDayComments(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	july := domain.NewYearMonth(2026, time.July)
	day := mustDate(t, "2026-07-14")

	if err := db.SetDayComment(ctx, day, "Betriebsausflug"); err != nil {
		t.Fatalf("SetDayComment: %v", err)
	}
	comments, err := db.DayComments(ctx, july)
	if err != nil {
		t.Fatalf("DayComments: %v", err)
	}
	if comments[day] != "Betriebsausflug" {
		t.Errorf("got %q, want %q", comments[day], "Betriebsausflug")
	}

	// One comment per day: writing again replaces rather than duplicates.
	if err := db.SetDayComment(ctx, day, "Ersetzt"); err != nil {
		t.Fatalf("SetDayComment replace: %v", err)
	}
	comments, _ = db.DayComments(ctx, july)
	if comments[day] != "Ersetzt" {
		t.Errorf("got %q after replace, want %q", comments[day], "Ersetzt")
	}
	if len(comments) != 1 {
		t.Errorf("got %d comments, want 1", len(comments))
	}

	// Blank clears, matching how clearing an hours cell behaves.
	if err := db.SetDayComment(ctx, day, "   "); err != nil {
		t.Fatalf("SetDayComment clear: %v", err)
	}
	comments, _ = db.DayComments(ctx, july)
	if len(comments) != 0 {
		t.Errorf("got %d comments after clearing, want 0", len(comments))
	}
}

func TestEmployeeRangeTotalAndEntries(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id := mustEmployee(t, db, "Worker", "2026-01-01", "")

	for _, spec := range []struct {
		day string
		v   domain.Centihours
	}{
		{"2026-07-01", 800},
		{"2026-07-15", 775},
		{"2026-08-01", 900},
	} {
		if err := db.SetHours(ctx, id, mustDate(t, spec.day), spec.v); err != nil {
			t.Fatalf("SetHours: %v", err)
		}
	}

	july := domain.NewYearMonth(2026, time.July)
	total, err := db.EmployeeRangeTotal(ctx, id, july.First(), july.Last())
	if err != nil {
		t.Fatalf("EmployeeRangeTotal: %v", err)
	}
	if total != 1575 {
		t.Errorf("July total %s, want 15.75", total)
	}

	entries, err := db.EmployeeEntries(ctx, id, july.First(), july.Last())
	if err != nil {
		t.Fatalf("EmployeeEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Date.After(entries[1].Date) {
		t.Error("entries are not in date order")
	}

	// An employee with nothing recorded totals zero rather than erroring.
	empty := mustEmployee(t, db, "Idle", "2026-01-01", "")
	if got, err := db.EmployeeRangeTotal(ctx, empty, july.First(), july.Last()); err != nil || got != 0 {
		t.Errorf("empty range total = %s, %v; want 0, nil", got, err)
	}
}

func TestEmployeeCRUD(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	id := mustEmployee(t, db, "Original", "2026-01-01", "")
	e, err := db.Employee(ctx, id)
	if err != nil {
		t.Fatalf("Employee: %v", err)
	}
	if e.DisplayName != "Original" || e.EndDate != nil {
		t.Errorf("round trip mismatch: %+v", e)
	}

	end := mustDate(t, "2026-12-31")
	e.DisplayName = "Renamed"
	e.EndDate = &end
	if err := db.UpdateEmployee(ctx, e); err != nil {
		t.Fatalf("UpdateEmployee: %v", err)
	}
	got, err := db.Employee(ctx, id)
	if err != nil {
		t.Fatalf("Employee after update: %v", err)
	}
	if got.DisplayName != "Renamed" || got.EndDate == nil || !got.EndDate.Equal(end) {
		t.Errorf("update not persisted: %+v", got)
	}

	if _, err := db.Employee(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing employee: got %v, want ErrNotFound", err)
	}
	if err := db.UpdateEmployee(ctx, domain.Employee{
		ID: 99999, DisplayName: "Ghost", StartDate: mustDate(t, "2026-01-01"),
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("updating missing employee: got %v, want ErrNotFound", err)
	}
	if err := db.DeleteEmployee(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting missing employee: got %v, want ErrNotFound", err)
	}
}

func TestCreateEmployeeRejectsInvalid(t *testing.T) {
	db := testDB(t)
	bad := []domain.Employee{
		{DisplayName: "", StartDate: mustDate(t, "2026-01-01")},
		{DisplayName: "NoStart"},
		{
			DisplayName: "Backwards",
			StartDate:   mustDate(t, "2026-07-01"),
			EndDate:     func() *domain.Date { d := mustDate(t, "2026-06-01"); return &d }(),
		},
	}
	for _, e := range bad {
		if _, err := db.CreateEmployee(t.Context(), e); err == nil {
			t.Errorf("CreateEmployee(%+v) succeeded, want validation error", e)
		}
	}
}
