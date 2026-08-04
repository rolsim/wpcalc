package httpx

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/report"
	"github.com/rolsim/wpcalc/internal/store"
)

type reportEmployee struct {
	Name       string
	MonthURL   string
	YearURL    string
	MonthLabel string
	YearLabel  string
}

type reportIndexView struct {
	view
	MonthLabel string
	MonthURL   string
	Month      domain.YearMonth
	PrevURL    string
	NextURL    string
	Employees  []reportEmployee
}

// handleReportIndex lists downloadable reports for the active tenant. A
// mandant-admin (or system-wide holder of print) sees every employee and the
// whole-tenant month PDF; anyone else sees only the employees they hold
// print-or-above on — this is the same route serving both "see every
// employee" and "download my own report", narrowed by permission rather than
// split into two pages.
func (s *Server) handleReportIndex(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.resolveActiveTenant(w, r)
	if !ok {
		return
	}
	month := domain.CurrentYearMonth()
	if raw := r.URL.Query().Get("m"); raw != "" {
		parsed, err := domain.ParseYearMonth(raw)
		if err != nil {
			s.renderError(w, r, http.StatusNotFound, "error.invalid_month")
			return
		}
		month = parsed
	}

	all, err := s.db.EmployeesActiveIn(r.Context(), tenantID, month)
	if err != nil {
		s.log.Error("report index", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	id, _ := auth.IdentityFrom(r.Context())
	canWholeTenant := id.CanInTenant(domain.PermPrint, tenantID)

	base := s.newView(r, "report.heading")
	base.TenantID = tenantID
	v := reportIndexView{
		view:       base,
		Month:      month,
		MonthLabel: base.T(i18nMonthKey(month)) + " " + strconv.Itoa(month.Year),
		PrevURL:    s.url(r, "/reports?m=%s", month.Prev()),
		NextURL:    s.url(r, "/reports?m=%s", month.Next()),
	}
	if canWholeTenant {
		v.MonthURL = s.url(r, "/report/month/%s.pdf", month)
	}
	for _, e := range all {
		if !id.Can(domain.PermPrint, e.ID, tenantID) {
			continue
		}
		v.Employees = append(v.Employees, reportEmployee{
			Name:       e.DisplayName,
			MonthURL:   s.url(r, "/report/employee/%d/month/%s.pdf", e.ID, month),
			YearURL:    s.url(r, "/report/employee/%d/year/%d.pdf", e.ID, month.Year),
			MonthLabel: base.T("report.employee_month"),
			YearLabel:  base.T("report.employee_year"),
		})
	}
	s.render(w, r, "reports.html", http.StatusOK, v)
}

func (s *Server) handleReportMonth(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.requireTenantPermission(w, r, domain.PermPrint)
	if !ok {
		return
	}
	month, ok := s.reportMonth(w, r, "ym")
	if !ok {
		return
	}
	s.writePDF(w, r, fmt.Sprintf("wpcalc-%s.pdf", month),
		func(rr *report.Renderer, buf *bytes.Buffer) error {
			return rr.MonthSummary(r.Context(), tenantID, month, buf)
		})
}

func (s *Server) handleReportEmployeeMonth(w http.ResponseWriter, r *http.Request) {
	id, ok := s.employeeIDFromPath(w, r)
	if !ok {
		return
	}
	if !s.canPrintEmployee(w, r, id) {
		return
	}
	month, ok := s.reportMonth(w, r, "ym")
	if !ok {
		return
	}
	s.writePDF(w, r, fmt.Sprintf("wpcalc-%d-%s.pdf", id, month),
		func(rr *report.Renderer, buf *bytes.Buffer) error {
			return rr.EmployeeMonth(r.Context(), id, month, buf)
		})
}

func (s *Server) handleReportEmployeeYear(w http.ResponseWriter, r *http.Request) {
	id, ok := s.employeeIDFromPath(w, r)
	if !ok {
		return
	}
	if !s.canPrintEmployee(w, r, id) {
		return
	}
	year, err := strconv.Atoi(trimPDF(r.PathValue("year")))
	if err != nil || year < 1000 || year > 9999 {
		s.renderError(w, r, http.StatusNotFound, "error.invalid_month")
		return
	}
	s.writePDF(w, r, fmt.Sprintf("wpcalc-%d-%d.pdf", id, year),
		func(rr *report.Renderer, buf *bytes.Buffer) error {
			return rr.EmployeeYear(r.Context(), id, year, buf)
		})
}

// canPrintEmployee checks print access against the employee's own tenant —
// deliberately independent of the caller's *active* tenant, so an account
// holding a role in more than one tenant can still reach a direct report
// link for any of them without switching first. Renders the failure itself.
func (s *Server) canPrintEmployee(w http.ResponseWriter, r *http.Request, employeeID int64) bool {
	emp, err := s.db.Employee(r.Context(), employeeID)
	if err != nil {
		s.employeeLookupError(w, r, err)
		return false
	}
	id, _ := auth.IdentityFrom(r.Context())
	if !id.Can(domain.PermPrint, employeeID, emp.TenantID) {
		s.renderError(w, r, http.StatusForbidden, "error.forbidden")
		return false
	}
	return true
}

// writePDF renders into a buffer before touching the response.
//
// Streaming straight out would commit a 200 and a PDF content type before a
// mid-render failure, leaving the browser to download a truncated file that
// looks like a successful export.
func (s *Server) writePDF(w http.ResponseWriter, r *http.Request, filename string, render func(*report.Renderer, *bytes.Buffer) error) {
	printer := s.printerFor(r)
	rr := report.New(s.db, printer)

	var buf bytes.Buffer
	if err := render(rr, &buf); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "error.not_found")
			return
		}
		s.log.Error("render report", "file", filename, "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	// inline so a click previews in the browser; the filename still applies on save.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}

// reportMonth parses a {ym} segment that may carry a .pdf suffix.
//
// Go's mux matches whole path segments only, so "{ym}.pdf" is not a pattern it
// can express. Accepting the suffix here keeps the documented URLs working.
func (s *Server) reportMonth(w http.ResponseWriter, r *http.Request, key string) (domain.YearMonth, bool) {
	month, err := domain.ParseYearMonth(trimPDF(r.PathValue(key)))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "error.invalid_month")
		return domain.YearMonth{}, false
	}
	return month, true
}

func trimPDF(s string) string { return strings.TrimSuffix(s, ".pdf") }
