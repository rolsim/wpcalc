package httpx

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

type employeeRow struct {
	domain.Employee
	StartDisplay string
	EndDisplay   string
	Active       bool
}

type employeeListView struct {
	view
	Employees []employeeRow
	NewURL    string
}

type employeeFormView struct {
	view
	// Form fields are carried as strings so a rejected submission redisplays
	// exactly what was typed rather than a value silently normalised on the
	// way through.
	ID        int64
	Name      string
	StartDate string
	EndDate   string
	IsNew     bool
	ActionURL string
	CancelURL string
}

func (s *Server) handleEmployeeList(w http.ResponseWriter, r *http.Request) {
	employees, err := s.db.Employees(r.Context())
	if err != nil {
		s.log.Error("list employees", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	v := employeeListView{view: s.newView(r, "employee.heading"), NewURL: s.url("/employees/new")}
	if key := r.URL.Query().Get("err"); key != "" {
		v.Error = v.T(errorKey(key))
	}

	today := domain.Today()
	for _, e := range employees {
		row := employeeRow{
			Employee:     e,
			StartDisplay: e.StartDate.Display(),
			EndDisplay:   v.T("employee.still_employed"),
			Active:       e.Employed(today),
		}
		if e.EndDate != nil {
			row.EndDisplay = e.EndDate.Display()
		}
		v.Employees = append(v.Employees, row)
	}
	s.render(w, "employees.html", http.StatusOK, v)
}

func (s *Server) handleEmployeeNew(w http.ResponseWriter, r *http.Request) {
	v := employeeFormView{
		view:      s.newView(r, "employee.new"),
		IsNew:     true,
		StartDate: domain.Today().String(),
		ActionURL: s.url("/employees"),
		CancelURL: s.url("/employees"),
	}
	s.render(w, "employee_form.html", http.StatusOK, v)
}

func (s *Server) handleEmployeeCreate(w http.ResponseWriter, r *http.Request) {
	e, errKey := s.employeeFromForm(r, 0)
	if errKey != "" {
		s.redisplayEmployeeForm(w, r, 0, true, errKey)
		return
	}
	if _, err := s.db.CreateEmployee(r.Context(), e); err != nil {
		s.log.Error("create employee", "error", err)
		s.redisplayEmployeeForm(w, r, 0, true, mapEmployeeError(err))
		return
	}
	http.Redirect(w, r, s.url("/employees"), http.StatusSeeOther)
}

func (s *Server) handleEmployeeEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := s.employeeIDFromPath(w, r)
	if !ok {
		return
	}
	e, err := s.db.Employee(r.Context(), id)
	if err != nil {
		s.employeeLookupError(w, r, err)
		return
	}

	v := employeeFormView{
		view:      s.newView(r, "employee.edit"),
		ID:        e.ID,
		Name:      e.DisplayName,
		StartDate: e.StartDate.String(),
		ActionURL: s.url("/employees/%d", e.ID),
		CancelURL: s.url("/employees"),
	}
	if e.EndDate != nil {
		v.EndDate = e.EndDate.String()
	}
	s.render(w, "employee_form.html", http.StatusOK, v)
}

func (s *Server) handleEmployeeUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.employeeIDFromPath(w, r)
	if !ok {
		return
	}
	e, errKey := s.employeeFromForm(r, id)
	if errKey != "" {
		s.redisplayEmployeeForm(w, r, id, false, errKey)
		return
	}
	if err := s.db.UpdateEmployee(r.Context(), e); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "error.not_found")
			return
		}
		s.log.Error("update employee", "error", err)
		s.redisplayEmployeeForm(w, r, id, false, mapEmployeeError(err))
		return
	}
	http.Redirect(w, r, s.url("/employees"), http.StatusSeeOther)
}

func (s *Server) handleEmployeeDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.employeeIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteEmployee(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "error.not_found")
			return
		}
		s.log.Error("delete employee", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	http.Redirect(w, r, s.url("/employees"), http.StatusSeeOther)
}

// employeeFromForm parses and validates a submission, returning a catalog key
// on failure rather than an error, because every failure here is a message
// shown next to the form.
func (s *Server) employeeFromForm(r *http.Request, id int64) (domain.Employee, string) {
	if err := r.ParseForm(); err != nil {
		return domain.Employee{}, "error.invalid_input"
	}

	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		return domain.Employee{}, "error.name_required"
	}

	start, err := domain.ParseDate(strings.TrimSpace(r.PostFormValue("start_date")))
	if err != nil {
		return domain.Employee{}, "error.invalid_date"
	}

	e := domain.Employee{ID: id, DisplayName: name, StartDate: start}

	// An empty end date is the normal case: still employed.
	if raw := strings.TrimSpace(r.PostFormValue("end_date")); raw != "" {
		end, err := domain.ParseDate(raw)
		if err != nil {
			return domain.Employee{}, "error.invalid_date"
		}
		e.EndDate = &end
	}

	if err := e.Validate(); err != nil {
		return domain.Employee{}, mapEmployeeError(err)
	}
	return e, ""
}

// redisplayEmployeeForm re-renders the form with the submitted values intact
// and the failure explained, rather than redirecting and losing the input.
func (s *Server) redisplayEmployeeForm(w http.ResponseWriter, r *http.Request, id int64, isNew bool, errKey string) {
	titleKey := "employee.edit"
	action := s.url("/employees/%d", id)
	if isNew {
		titleKey = "employee.new"
		action = s.url("/employees")
	}

	v := employeeFormView{
		view:      s.newView(r, titleKey),
		ID:        id,
		Name:      r.PostFormValue("name"),
		StartDate: r.PostFormValue("start_date"),
		EndDate:   r.PostFormValue("end_date"),
		IsNew:     isNew,
		ActionURL: action,
		CancelURL: s.url("/employees"),
	}
	v.Error = v.T(errKey)
	s.render(w, "employee_form.html", http.StatusUnprocessableEntity, v)
}

func mapEmployeeError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrInvalidEmployee):
		msg := err.Error()
		switch {
		case strings.Contains(msg, "display name"):
			return "error.name_required"
		case strings.Contains(msg, "before start date"):
			return "error.end_before_start"
		default:
			return "error.invalid_input"
		}
	default:
		return "error.server"
	}
}

func (s *Server) employeeIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.renderError(w, r, http.StatusNotFound, "error.not_found")
		return 0, false
	}
	return id, true
}

func (s *Server) employeeLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "error.not_found")
		return
	}
	s.log.Error("employee lookup", "error", err)
	s.renderError(w, r, http.StatusInternalServerError, "error.server")
}
