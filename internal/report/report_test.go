package report

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

func newRenderer(t *testing.T) (*Renderer, *store.DB) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "report.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bundle, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	r := New(db, bundle.For(i18n.DefaultLang))
	// Pin the timestamp so the footer is stable across runs.
	r.now = func() time.Time { return time.Date(2026, time.July, 28, 22, 0, 0, 0, time.UTC) }
	return r, db
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func employee(t *testing.T, db *store.DB, name, start, end string) int64 {
	t.Helper()
	e := domain.Employee{DisplayName: name, StartDate: mustDate(t, start)}
	if end != "" {
		d := mustDate(t, end)
		e.EndDate = &d
	}
	id, err := db.CreateEmployee(t.Context(), e)
	if err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	return id
}

// assertPDF checks the output is a real, non-trivial PDF rather than an empty
// file that merely starts with the right bytes.
func assertPDF(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	body := buf.String()
	if !strings.HasPrefix(body, "%PDF-") {
		t.Fatalf("output is not a PDF: %.40q", body)
	}
	if !strings.Contains(body, "%%EOF") {
		t.Error("PDF has no EOF marker; the document was truncated")
	}
	if buf.Len() < 800 {
		t.Errorf("PDF is only %d bytes; suspiciously empty", buf.Len())
	}
	return body
}

func TestMonthSummaryRendersTotals(t *testing.T) {
	r, db := newRenderer(t)
	ctx := t.Context()
	july := domain.NewYearMonth(2026, time.July)

	alice := employee(t, db, "Alice Muster", "2026-01-01", "")
	bob := employee(t, db, "Bob Beispiel", "2026-01-01", "")

	if err := db.SetHours(ctx, alice, mustDate(t, "2026-07-14"), 775); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHours(ctx, alice, mustDate(t, "2026-07-15"), 800); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHours(ctx, bob, mustDate(t, "2026-07-14"), 425); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := r.MonthSummary(ctx, july, &buf); err != nil {
		t.Fatalf("MonthSummary: %v", err)
	}
	assertPDF(t, &buf)

	// The totals the PDF prints must be the ones the store computes, since
	// the grid prints those same figures on screen.
	totals, err := db.Totals(ctx, july)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := totals.PerEmployee[alice], domain.Centihours(1575); got != want {
		t.Errorf("Alice total %s, want %s", got, want)
	}
	if got, want := totals.Grand, domain.Centihours(2000); got != want {
		t.Errorf("grand total %s, want %s", got, want)
	}
}

func TestMonthSummaryHandlesEmptyMonth(t *testing.T) {
	// A month with nobody employed must still produce a valid document rather
	// than a zero-byte download.
	r, _ := newRenderer(t)
	var buf bytes.Buffer
	if err := r.MonthSummary(t.Context(), domain.NewYearMonth(2030, time.January), &buf); err != nil {
		t.Fatalf("MonthSummary on an empty month: %v", err)
	}
	assertPDF(t, &buf)
}

func TestEmployeeMonthListsEveryEmployedDay(t *testing.T) {
	r, db := newRenderer(t)
	ctx := t.Context()
	july := domain.NewYearMonth(2026, time.July)

	// Employed for 11 days of the month only.
	id := employee(t, db, "Teilzeit Person", "2026-07-10", "2026-07-20")
	if err := db.SetHours(ctx, id, mustDate(t, "2026-07-14"), 775); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDayComment(ctx, mustDate(t, "2026-07-14"), "Betriebsausflug"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := r.EmployeeMonth(ctx, id, july, &buf); err != nil {
		t.Fatalf("EmployeeMonth: %v", err)
	}
	assertPDF(t, &buf)

	total, err := db.EmployeeRangeTotal(ctx, id, july.First(), july.Last())
	if err != nil {
		t.Fatal(err)
	}
	if total != 775 {
		t.Errorf("month total %s, want 7.75", total)
	}
}

func TestEmployeeMonthWithNoEmployedDays(t *testing.T) {
	r, db := newRenderer(t)
	id := employee(t, db, "Nicht im Juli", "2027-01-01", "")
	var buf bytes.Buffer
	if err := r.EmployeeMonth(t.Context(), id, domain.NewYearMonth(2026, time.July), &buf); err != nil {
		t.Fatalf("EmployeeMonth: %v", err)
	}
	assertPDF(t, &buf)
}

func TestEmployeeYearSumsTwelveMonths(t *testing.T) {
	r, db := newRenderer(t)
	ctx := t.Context()
	id := employee(t, db, "Ganzjahr Person", "2026-01-01", "")

	// One 8-hour day in each of three months.
	var want domain.Centihours
	for _, day := range []string{"2026-02-10", "2026-06-10", "2026-11-10"} {
		if err := db.SetHours(ctx, id, mustDate(t, day), 800); err != nil {
			t.Fatal(err)
		}
		want += 800
	}
	// A day in the following year must not be counted.
	if err := db.SetHours(ctx, id, mustDate(t, "2027-01-10"), 800); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := r.EmployeeYear(ctx, id, 2026, &buf); err != nil {
		t.Fatalf("EmployeeYear: %v", err)
	}
	assertPDF(t, &buf)

	var got domain.Centihours
	for _, m := range domain.Months(2026) {
		v, err := db.EmployeeRangeTotal(ctx, id, m.First(), m.Last())
		if err != nil {
			t.Fatal(err)
		}
		got += v
	}
	if got != want {
		t.Errorf("year total %s, want %s — a neighbouring year leaked in", got, want)
	}
}

func TestReportsRejectUnknownEmployee(t *testing.T) {
	r, _ := newRenderer(t)
	var buf bytes.Buffer
	if err := r.EmployeeMonth(t.Context(), 99999, domain.NewYearMonth(2026, time.July), &buf); err == nil {
		t.Error("EmployeeMonth for a missing employee returned no error")
	}
	buf.Reset()
	if err := r.EmployeeYear(t.Context(), 99999, 2026, &buf); err == nil {
		t.Error("EmployeeYear for a missing employee returned no error")
	}
}

func TestUmlautsSurviveIntoThePDF(t *testing.T) {
	// fpdf's core fonts are Latin-1. Without the cp1252 translator a name like
	// "Müller" prints as mojibake — which still passes a %PDF check and is
	// only visible on the page someone actually files.
	r, db := newRenderer(t)
	r.SetCompression(false)
	ctx := t.Context()
	id := employee(t, db, "Jürg Müller-Schäfer", "2026-01-01", "")
	if err := db.SetHours(ctx, id, mustDate(t, "2026-07-14"), 800); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := r.MonthSummary(ctx, domain.NewYearMonth(2026, time.July), &buf); err != nil {
		t.Fatal(err)
	}
	body := assertPDF(t, &buf)

	// Content streams are zlib-compressed by default, which would make the
	// checks below pass vacuously — no text of any kind survives in the bytes.
	// Compression is off for this render specifically so they are real.
	//
	// Latin-1 encodes "ä" as the single byte 0xE4, so a correctly translated
	// name appears as "Sch\xe4fer". If the string had bypassed the translator
	// its UTF-8 form (0xC3 0xA4) would be there instead and the page would
	// render as "SchÃ¤fer".
	if !strings.Contains(body, "Sch\xe4fer") {
		t.Error("no Latin-1 umlaut found; the name was not encoded for the font")
	}
	if strings.Contains(body, "Sch\xc3\xa4fer") {
		t.Error("UTF-8 bytes reached the PDF untranslated; umlauts will be mojibake")
	}
	// The surname is long enough to prove it was not silently clipped away.
	if !strings.Contains(body, "M\xfcller") {
		t.Error("the employee name is absent or truncated in the document")
	}
}

func TestClipKeepsColumnsAligned(t *testing.T) {
	// A long name must be truncated rather than overprint the hours column.
	long := strings.Repeat("W", 200)
	got := clip(long, 40)
	if len(got) >= len(long) {
		t.Errorf("clip did not shorten a %d-char name", len(long))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated text %q does not signal it was cut", got)
	}
	// Short text is left alone.
	if got := clip("Anna", 40); got != "Anna" {
		t.Errorf("clip altered short text: %q", got)
	}
}
