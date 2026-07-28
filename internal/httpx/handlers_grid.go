package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

// gridCell is one crossing point of day and employee.
type gridCell struct {
	EmployeeID int64
	DateISO    string
	Hours      domain.Centihours
	// Locked marks a day outside this employee's employment. The template
	// greys and disables it, but that is presentation: the write path checks
	// the same rule again, because a disabled input is a suggestion.
	Locked bool
}

// gridRow is one calendar day across every visible employee.
type gridRow struct {
	Date      domain.Date
	DateISO   string
	Display   string
	Weekday   string
	IsWeekend bool
	IsToday   bool
	Cells     []gridCell
	Comment   string
	Total     domain.Centihours
}

type gridView struct {
	view
	Month      domain.YearMonth
	MonthLabel string
	PrevURL    string
	NextURL    string
	TodayURL   string
	Employees  []domain.Employee
	Rows       []gridRow
	// EmployeeTotals is positionally aligned with Employees.
	EmployeeTotals []domain.Centihours
	GrandTotal     domain.Centihours
	Sep            string
	HasEmployees   bool
}

func (s *Server) handleGrid(w http.ResponseWriter, r *http.Request) {
	month, ok := s.monthFromPath(w, r)
	if !ok {
		return
	}

	v, err := s.buildGridView(r, month)
	if err != nil {
		s.log.Error("build grid", "month", month, "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	// Surface a validation failure carried back from a redirect after a
	// rejected write, so the no-JS path still explains itself.
	if key := r.URL.Query().Get("err"); key != "" {
		v.Error = v.T(errorKey(key))
	}

	s.render(w, r, "grid.html", http.StatusOK, v)
}

// buildGridView assembles a month from four queries rather than one per cell.
func (s *Server) buildGridView(r *http.Request, month domain.YearMonth) (gridView, error) {
	ctx := r.Context()

	employees, err := s.db.EmployeesActiveIn(ctx, month)
	if err != nil {
		return gridView{}, err
	}
	entries, err := s.db.MonthEntries(ctx, month)
	if err != nil {
		return gridView{}, err
	}
	comments, err := s.db.DayComments(ctx, month)
	if err != nil {
		return gridView{}, err
	}
	totals, err := s.db.Totals(ctx, month)
	if err != nil {
		return gridView{}, err
	}

	base := s.newView(r, "app.title")
	v := gridView{
		view:         base,
		Month:        month,
		MonthLabel:   base.T(i18nMonthKey(month)) + " " + strconv.Itoa(month.Year),
		PrevURL:      s.url("/m/%s", month.Prev()),
		NextURL:      s.url("/m/%s", month.Next()),
		TodayURL:     s.url("/m/%s", domain.CurrentYearMonth()),
		Employees:    employees,
		Sep:          base.DecimalSep(),
		HasEmployees: len(employees) > 0,
	}

	today := domain.Today()
	for _, day := range month.Days() {
		row := gridRow{
			Date:      day,
			DateISO:   day.String(),
			Display:   day.Display(),
			Weekday:   base.T(i18nWeekdayKey(day)),
			IsWeekend: day.IsWeekend(),
			IsToday:   day.Equal(today),
			Comment:   comments[day],
			Total:     totals.PerDay[day],
			Cells:     make([]gridCell, 0, len(employees)),
		}
		for _, e := range employees {
			row.Cells = append(row.Cells, gridCell{
				EmployeeID: e.ID,
				DateISO:    day.String(),
				Hours:      entries[e.ID][day],
				Locked:     !e.Employed(day),
			})
		}
		v.Rows = append(v.Rows, row)
	}

	v.EmployeeTotals = make([]domain.Centihours, 0, len(employees))
	for _, e := range employees {
		v.EmployeeTotals = append(v.EmployeeTotals, totals.PerEmployee[e.ID])
	}
	v.GrandTotal = totals.Grand

	return v, nil
}

// handleSetHours records one cell.
//
// It serves both the no-JS form post and the enhanced fetch call. The no-JS
// path is the one that must work: it redirects back to the month, carrying any
// validation failure in the query string. The JSON path is an optimisation
// over exactly the same validation and storage.
func (s *Server) handleSetHours(w http.ResponseWriter, r *http.Request) {
	month, ok := s.monthFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeSetResult(w, r, month, "", "error.invalid_input", http.StatusBadRequest)
		return
	}

	employeeID, err := strconv.ParseInt(r.PostFormValue("employee_id"), 10, 64)
	if err != nil {
		s.writeSetResult(w, r, month, "", "error.invalid_input", http.StatusBadRequest)
		return
	}
	day, err := domain.ParseDate(r.PostFormValue("date"))
	if err != nil {
		s.writeSetResult(w, r, month, "", "error.invalid_date", http.StatusBadRequest)
		return
	}

	hours, err := domain.ParseHours(r.PostFormValue("hours"))
	if err != nil {
		key := "error.invalid_hours"
		if errors.Is(err, domain.ErrHoursRange) {
			key = "error.hours_range"
		}
		s.writeSetResult(w, r, month, "", key, http.StatusUnprocessableEntity)
		return
	}

	if err := s.db.SetHours(r.Context(), employeeID, day, hours); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotEmployed):
			// The template greys this cell; reaching here means the request
			// did not come from that template.
			s.log.Warn("write to a locked cell rejected",
				"employee", employeeID, "date", day, "remote", r.RemoteAddr)
			s.writeSetResult(w, r, month, "", "error.not_employed", http.StatusUnprocessableEntity)
		case errors.Is(err, domain.ErrHoursRange):
			s.writeSetResult(w, r, month, "", "error.hours_range", http.StatusUnprocessableEntity)
		case errors.Is(err, store.ErrNotFound):
			s.writeSetResult(w, r, month, "", "error.not_found", http.StatusNotFound)
		default:
			s.log.Error("set hours", "error", err)
			s.writeSetResult(w, r, month, "", "error.server", http.StatusInternalServerError)
		}
		return
	}

	s.writeSetResult(w, r, month, hours.Format(s.sepFor(r)), "", http.StatusOK)
}

func (s *Server) handleSetComment(w http.ResponseWriter, r *http.Request) {
	month, ok := s.monthFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeSetResult(w, r, month, "", "error.invalid_input", http.StatusBadRequest)
		return
	}
	day, err := domain.ParseDate(r.PostFormValue("date"))
	if err != nil {
		s.writeSetResult(w, r, month, "", "error.invalid_date", http.StatusBadRequest)
		return
	}
	if err := s.db.SetDayComment(r.Context(), day, r.PostFormValue("comment")); err != nil {
		s.log.Error("set comment", "error", err)
		s.writeSetResult(w, r, month, "", "error.server", http.StatusInternalServerError)
		return
	}
	s.writeSetResult(w, r, month, "", "", http.StatusOK)
}

// setResult is the JSON body the enhanced path receives. It carries the
// recomputed accumulators so the browser never adds up numbers itself — the
// totals on screen always come from the same SQL the printed ones do.
type setResult struct {
	OK            bool   `json:"ok"`
	Value         string `json:"value,omitempty"`
	Error         string `json:"error,omitempty"`
	EmployeeTotal string `json:"employeeTotal,omitempty"`
	DayTotal      string `json:"dayTotal,omitempty"`
	GrandTotal    string `json:"grandTotal,omitempty"`
}

func (s *Server) writeSetResult(w http.ResponseWriter, r *http.Request, month domain.YearMonth, value, errKey string, status int) {
	if !wantsJSON(r) {
		target := s.url("/m/%s", month)
		if errKey != "" {
			target += "?err=" + shortErrorKey(errKey)
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}

	v := s.bundle.For(s.bundle.Match(r.Header.Get("Accept-Language")))
	res := setResult{OK: errKey == "", Value: value}
	if errKey != "" {
		res.Error = v.T(errKey)
	} else if totals, err := s.db.Totals(r.Context(), month); err == nil {
		sep := v.DecimalSep()
		res.GrandTotal = totals.Grand.Format(sep)
		if id, err := strconv.ParseInt(r.PostFormValue("employee_id"), 10, 64); err == nil {
			res.EmployeeTotal = totals.PerEmployee[id].Format(sep)
		}
		if day, err := domain.ParseDate(r.PostFormValue("date")); err == nil {
			res.DayTotal = totals.PerDay[day].Format(sep)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(res)
}

// monthFromPath parses the {ym} path segment.
func (s *Server) monthFromPath(w http.ResponseWriter, r *http.Request) (domain.YearMonth, bool) {
	month, err := domain.ParseYearMonth(r.PathValue("ym"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "error.invalid_month")
		return domain.YearMonth{}, false
	}
	return month, true
}

func (s *Server) sepFor(r *http.Request) string {
	return s.bundle.For(s.bundle.Match(r.Header.Get("Accept-Language"))).DecimalSep()
}
