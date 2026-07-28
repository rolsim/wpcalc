// Package report renders the PDF timesheets.
//
// Every figure here comes from the same store queries the grid uses. Nothing
// is recomputed: a printed total that disagrees with the screen is worse than
// no report at all, because it is the printed one that gets filed.
package report

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/go-pdf/fpdf"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

// Source is the slice of the store the reports need.
type Source interface {
	Employee(ctx context.Context, id int64) (domain.Employee, error)
	EmployeesActiveIn(ctx context.Context, m domain.YearMonth) ([]domain.Employee, error)
	Totals(ctx context.Context, m domain.YearMonth) (store.MonthTotals, error)
	EmployeeEntries(ctx context.Context, employeeID int64, from, to domain.Date) ([]domain.TimeEntry, error)
	EmployeeRangeTotal(ctx context.Context, employeeID int64, from, to domain.Date) (domain.Centihours, error)
	DayComments(ctx context.Context, m domain.YearMonth) (map[domain.Date]string, error)
}

// Renderer produces the PDFs for one locale.
type Renderer struct {
	src Source
	p   *i18n.Printer
	// now is injectable so the golden-ish tests are not time-dependent.
	now func() time.Time
	// compress mirrors fpdf's content-stream compression.
	compress bool
}

// New builds a Renderer.
func New(src Source, p *i18n.Printer) *Renderer {
	return &Renderer{src: src, p: p, now: time.Now, compress: true}
}

// SetCompression toggles content-stream compression.
//
// Compression is on for anything a user downloads. Turning it off makes the
// text inspectable, which is the only way a test can check that strings were
// actually encoded for the font rather than merely that a PDF was produced.
func (r *Renderer) SetCompression(on bool) { r.compress = on }

// Page geometry, in mm on A4 portrait.
const (
	marginX    = 15.0
	marginTop  = 18.0
	lineHeight = 6.0
)

// doc wraps fpdf with the bits every report shares.
type doc struct {
	pdf   *fpdf.Fpdf
	tr    func(string) string
	title string
	sep   string
	p     *i18n.Printer
}

// newDoc sets up the page, header, and footer.
//
// fpdf's built-in fonts are Latin-1, so every string goes through the cp1252
// translator. Without it "Mitarbeiter Müller" prints as mojibake — which is
// exactly the kind of thing that looks fine in a test asserting %PDF and
// wrong on the page someone files.
func newDoc(p *i18n.Printer, title, subtitle string, generated time.Time, compress bool) *doc {
	pdf := fpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	d := &doc{pdf: pdf, tr: tr, title: title, sep: p.DecimalSep(), p: p}

	pdf.SetCompression(compress)
	pdf.SetMargins(marginX, marginTop, marginX)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AliasNbPages("{nb}")

	pdf.SetHeaderFunc(func() {
		pdf.SetFont("Helvetica", "B", 13)
		pdf.CellFormat(0, 7, tr(title), "", 1, "L", false, 0, "")
		if subtitle != "" {
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(90, 90, 90)
			pdf.CellFormat(0, 5, tr(subtitle), "", 1, "L", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
		}
		pdf.Ln(2)
	})

	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(0, 5, tr(p.T("report.generated_at", generated.Format("02.01.2006 15:04"))),
			"", 0, "L", false, 0, "")
		pdf.CellFormat(0, 5, tr(p.T("report.page", pdf.PageNo()))+" / {nb}",
			"", 0, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})

	pdf.AddPage()
	return d
}

// column describes one table column.
type column struct {
	title string
	width float64
	align string
}

func (d *doc) header(cols []column) {
	d.pdf.SetFont("Helvetica", "B", 9)
	d.pdf.SetFillColor(235, 238, 242)
	for _, c := range cols {
		d.pdf.CellFormat(c.width, lineHeight, d.tr(c.title), "B", 0, c.align, true, 0, "")
	}
	d.pdf.Ln(-1)
	d.pdf.SetFont("Helvetica", "", 9)
}

func (d *doc) row(cols []column, cells []string, shaded bool) {
	if shaded {
		d.pdf.SetFillColor(246, 247, 249)
	}
	for i, c := range cols {
		text := ""
		if i < len(cells) {
			text = cells[i]
		}
		d.pdf.CellFormat(c.width, lineHeight, d.tr(clip(text, c.width)), "B", 0, c.align, shaded, 0, "")
	}
	d.pdf.Ln(-1)
}

func (d *doc) totalRow(cols []column, cells []string) {
	d.pdf.SetFont("Helvetica", "B", 9)
	d.pdf.SetFillColor(226, 231, 237)
	for i, c := range cols {
		text := ""
		if i < len(cells) {
			text = cells[i]
		}
		d.pdf.CellFormat(c.width, lineHeight+1, d.tr(text), "T", 0, c.align, true, 0, "")
	}
	d.pdf.Ln(-1)
	d.pdf.SetFont("Helvetica", "", 9)
}

func (d *doc) note(text string) {
	d.pdf.SetFont("Helvetica", "I", 9)
	d.pdf.SetTextColor(110, 110, 110)
	d.pdf.CellFormat(0, lineHeight, d.tr(text), "", 1, "L", false, 0, "")
	d.pdf.SetTextColor(0, 0, 0)
	d.pdf.SetFont("Helvetica", "", 9)
}

func (d *doc) output(w io.Writer) error {
	if err := d.pdf.Output(w); err != nil {
		return fmt.Errorf("report: render pdf: %w", err)
	}
	return nil
}

// clip trims text that would overflow its column, so a long name pushes the
// numbers out of alignment instead of overprinting the next cell.
func clip(s string, width float64) string {
	// ~1.9mm per character at 9pt Helvetica is close enough for a table cell.
	maxChars := int(width / 1.9)
	if maxChars <= 1 || len(s) <= maxChars {
		return s
	}
	if maxChars <= 2 {
		return s[:maxChars]
	}
	return s[:maxChars-1] + "…"
}

// MonthSummary lists how many hours each employee booked in the month.
func (r *Renderer) MonthSummary(ctx context.Context, m domain.YearMonth, w io.Writer) error {
	employees, err := r.src.EmployeesActiveIn(ctx, m)
	if err != nil {
		return err
	}
	totals, err := r.src.Totals(ctx, m)
	if err != nil {
		return err
	}

	d := newDoc(r.p, r.p.T("report.month_overview"), r.monthLabel(m), r.now(), r.compress)
	cols := []column{
		{r.p.T("report.employee"), 120, "L"},
		{r.p.T("report.hours"), 40, "R"},
	}

	if len(employees) == 0 {
		d.note(r.p.T("report.no_data"))
		return d.output(w)
	}

	d.header(cols)
	for i, e := range employees {
		d.row(cols, []string{
			e.DisplayName,
			totals.PerEmployee[e.ID].Format(d.sep),
		}, i%2 == 1)
	}
	// The grand total comes from Totals, not from re-adding the column above,
	// so the report cannot disagree with the grid about the same month.
	d.totalRow(cols, []string{r.p.T("report.total"), totals.Grand.Format(d.sep)})

	return d.output(w)
}

// EmployeeMonth is one person's day-by-day timesheet for a month.
func (r *Renderer) EmployeeMonth(ctx context.Context, employeeID int64, m domain.YearMonth, w io.Writer) error {
	e, err := r.src.Employee(ctx, employeeID)
	if err != nil {
		return err
	}
	entries, err := r.src.EmployeeEntries(ctx, employeeID, m.First(), m.Last())
	if err != nil {
		return err
	}
	comments, err := r.src.DayComments(ctx, m)
	if err != nil {
		return err
	}
	total, err := r.src.EmployeeRangeTotal(ctx, employeeID, m.First(), m.Last())
	if err != nil {
		return err
	}

	byDay := make(map[domain.Date]domain.Centihours, len(entries))
	for _, en := range entries {
		byDay[en.Date] = en.Hours
	}

	d := newDoc(r.p, e.DisplayName, r.monthLabel(m), r.now(), r.compress)
	cols := []column{
		{r.p.T("report.date"), 26, "L"},
		{r.p.T("grid.weekday"), 14, "L"},
		{r.p.T("report.hours"), 22, "R"},
		{r.p.T("report.comment"), 118, "L"},
	}
	d.header(cols)

	// Every day of employment within the month is listed, including the empty
	// ones: a timesheet with gaps silently omitted cannot be checked against
	// a calendar.
	printed := 0
	for _, day := range m.Days() {
		if !e.Employed(day) {
			continue
		}
		printed++
		hours := ""
		if v, ok := byDay[day]; ok {
			hours = v.Format(d.sep)
		}
		d.row(cols, []string{
			day.Display(),
			r.p.T(i18n.WeekdayKey(day.Weekday())),
			hours,
			comments[day],
		}, day.IsWeekend())
	}
	if printed == 0 {
		d.note(r.p.T("report.no_data"))
		return d.output(w)
	}

	d.totalRow(cols, []string{r.p.T("report.total"), "", total.Format(d.sep), ""})
	return d.output(w)
}

// EmployeeYear is one person's month-by-month total for a calendar year.
func (r *Renderer) EmployeeYear(ctx context.Context, employeeID int64, year int, w io.Writer) error {
	e, err := r.src.Employee(ctx, employeeID)
	if err != nil {
		return err
	}

	d := newDoc(r.p, e.DisplayName, strconv.Itoa(year), r.now(), r.compress)
	cols := []column{
		{r.p.T("report.month"), 120, "L"},
		{r.p.T("report.hours"), 40, "R"},
	}
	d.header(cols)

	var yearTotal domain.Centihours
	for i, m := range domain.Months(year) {
		total, err := r.src.EmployeeRangeTotal(ctx, employeeID, m.First(), m.Last())
		if err != nil {
			return err
		}
		yearTotal += total

		hours := ""
		if !e.ActiveIn(m) {
			// Distinguish "not employed" from "employed and booked nothing";
			// a bare 0.00 for both would misrepresent the year.
			hours = "–"
		} else {
			hours = total.Format(d.sep)
		}
		d.row(cols, []string{r.p.T(i18n.MonthKey(m.Month)), hours}, i%2 == 1)
	}

	d.totalRow(cols, []string{r.p.T("report.total"), yearTotal.Format(d.sep)})
	return d.output(w)
}

func (r *Renderer) monthLabel(m domain.YearMonth) string {
	return r.p.T(i18n.MonthKey(m.Month)) + " " + strconv.Itoa(m.Year)
}
