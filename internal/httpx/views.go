package httpx

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"source.simonet.internal/rolsim/wpcalc/internal/auth"
	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
)

// pageTemplates are the content templates, each rendered inside base.html.
//
// They are parsed into one template set per page rather than into a single
// shared set: every page defines a block named "content", so a shared set
// would silently keep only whichever file parsed last.
var pageTemplates = []string{
	"grid.html",
	"employees.html",
	"employee_form.html",
	"login.html",
	"error.html",
	"reports.html",
}

// view is embedded in every page's data, which promotes T onto the template
// dot. Templates therefore write {{ .T "grid.date" }} directly, with no way to
// reach a raw string without noticing.
type view struct {
	*i18n.Printer
	Title    string
	Base     string
	Identity auth.Identity
	Error    string
	Flash    string
}

// URL builds an absolute path, honouring the base path the WordPress admin
// page mounts the app under.
func (v view) URL(format string, args ...any) string {
	return joinBase(v.Base, fmt.Sprintf(format, args...))
}

// LoggedIn reports whether a session is active, so the layout can hide the
// logout control on the login page.
func (v view) LoggedIn() bool { return !v.Identity.IsZero() }

// newView assembles the shared part of every page's data.
func (s *Server) newView(r *http.Request, titleKey string) view {
	p := s.bundle.For(s.bundle.Match(r.Header.Get("Accept-Language")))
	id, _ := auth.IdentityFrom(r.Context())
	return view{
		Printer:  p,
		Title:    p.T(titleKey),
		Base:     s.basePath,
		Identity: id,
	}
}

// funcMap holds the helpers templates need that are not methods on the data.
func (s *Server) funcMap() template.FuncMap {
	return template.FuncMap{
		// hours renders centihours with the locale separator. Templates must
		// never format hours themselves; that is how two views of the same
		// number end up disagreeing.
		"hours": func(c domain.Centihours, sep string) string { return c.Format(sep) },

		// hoursOrBlank leaves an empty cell empty rather than printing 0.00,
		// so an untouched cell is visually distinct from a recorded zero.
		"hoursOrBlank": func(c domain.Centihours, sep string) string {
			if c == 0 {
				return ""
			}
			return c.Format(sep)
		},
	}
}

// render executes a page into a buffer first.
//
// Writing straight to the ResponseWriter would commit a 200 and half a page
// before a template error surfaced, leaving the browser with something that
// looks like a successful but broken render.
func (s *Server) render(w http.ResponseWriter, page string, status int, data any) {
	tmpl, ok := s.pages[page]
	if !ok {
		s.log.Error("unknown page template", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		s.log.Error("render failed", "page", page, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderError shows a message page. Callers pass a catalog key so the message
// is translated like everything else.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, msgKey string) {
	v := s.newView(r, "app.title")
	v.Error = v.T(msgKey)
	s.render(w, "error.html", status, struct{ view }{v})
}

// url builds a server-side path, honouring the base path.
func (s *Server) url(format string, args ...any) string {
	return joinBase(s.basePath, fmt.Sprintf(format, args...))
}

// normaliseBase reduces a configured base path to either "" or "/prefix".
func normaliseBase(base string) string {
	base = strings.TrimSpace(base)
	base = strings.TrimSuffix(base, "/")
	if base == "" || base == "/" {
		return ""
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return base
}

func joinBase(base, path string) string {
	if base == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// currentMonth is the month the grid opens on.
func currentMonth() domain.YearMonth { return domain.CurrentYearMonth() }

func i18nMonthKey(m domain.YearMonth) string { return i18n.MonthKey(m.Month) }
func i18nWeekdayKey(d domain.Date) string    { return i18n.WeekdayKey(d.Weekday()) }

// knownErrors is the set of validation failures that may round-trip through a
// redirect's query string.
//
// The ?err= value is client-controlled. Mapping it through a fixed table
// rather than concatenating "error."+value stops a crafted link from rendering
// an arbitrary catalog entry as if the server had produced it.
var knownErrors = map[string]string{
	"invalid_input":    "error.invalid_input",
	"invalid_hours":    "error.invalid_hours",
	"hours_range":      "error.hours_range",
	"not_employed":     "error.not_employed",
	"invalid_date":     "error.invalid_date",
	"invalid_month":    "error.invalid_month",
	"not_found":        "error.not_found",
	"name_required":    "error.name_required",
	"end_before_start": "error.end_before_start",
	"server":           "error.server",
}

// errorKey maps a query-string token to a catalog key, falling back to a
// generic message for anything unrecognised.
func errorKey(short string) string {
	if key, ok := knownErrors[short]; ok {
		return key
	}
	return "error.invalid_input"
}

// shortErrorKey is the inverse, for building the redirect.
func shortErrorKey(full string) string {
	return strings.TrimPrefix(full, "error.")
}
