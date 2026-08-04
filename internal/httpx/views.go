package httpx

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/i18n"
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
	"tenants.html",
	"tenant_choose.html",
	"tenant_access.html",
	"roles.html",
}

// view is embedded in every page's data, which promotes T onto the template
// dot. Templates therefore write {{ .T "grid.date" }} directly, with no way to
// reach a raw string without noticing.
type view struct {
	*i18n.Printer
	Title     string
	Base      string
	LinkParam string
	Fragment  bool
	Identity  auth.Identity
	Error     string
	Flash     string

	// Languages is empty when the authenticator cannot store a preference, so
	// the layout hides a control that would otherwise appear to work and not.
	Languages   []LanguageOption
	LanguageURL string
	// CurrentPath is where to return after changing the language, so the
	// change does not also lose the month someone was looking at.
	CurrentPath string

	// TenantID is the active tenant a tenant-scoped page (grid, employees,
	// reports) was built for. Zero on pages that are not scoped to one
	// tenant (the tenant list, role management, the chooser itself).
	TenantID int64
	// AccessibleTenants drives the topbar switcher — populated only when the
	// account can act in more than one, so the layout hides a switcher that
	// would otherwise have nothing to switch between.
	AccessibleTenants []TenantOption
	TenantSwitchURL   string
}

// LanguageOption is one entry in the language selector.
type LanguageOption struct {
	Tag      string
	Label    string
	Selected bool
}

// TenantOption is one entry in the tenant switcher.
type TenantOption struct {
	ID       int64
	Name     string
	Selected bool
}

// URL builds an absolute path, honouring the base path the WordPress admin
// page mounts the app under.
func (v view) URL(format string, args ...any) string {
	return buildURL(v.Base, v.LinkParam, fmt.Sprintf(format, args...))
}

// LoggedIn reports whether a session is active, so the layout can hide the
// logout control on the login page.
func (v view) LoggedIn() bool { return !v.Identity.IsZero() }

// printerFor resolves the locale for one request.
//
// A stored preference wins over the browser, because it is the more specific
// statement: someone whose laptop is set to English but who wants the German
// interface has said so explicitly. An unrecognised preference — a catalog
// that has since been removed, say — falls through to negotiation rather than
// rendering markers, which is why the check is Has() and not a bare read.
func (s *Server) printerFor(r *http.Request) *i18n.Printer {
	if id, ok := auth.IdentityFrom(r.Context()); ok && s.bundle.Has(id.Language) {
		return s.bundle.For(id.Language)
	}
	return s.bundle.For(s.bundle.Match(r.Header.Get("Accept-Language")))
}

// newView assembles the shared part of every page's data.
func (s *Server) newView(r *http.Request, titleKey string) view {
	p := s.printerFor(r)
	id, _ := auth.IdentityFrom(r.Context())
	base, linkParam := s.mountFor(r)
	v := view{
		Printer:   p,
		Title:     p.T(titleKey),
		Base:      base,
		LinkParam: linkParam,
		Fragment:  isFragment(r),
		Identity:  id,
	}
	if _, ok := s.authn.(auth.LanguageWriter); ok && !id.IsZero() {
		v.LanguageURL = s.url(r, "/language")
		v.CurrentPath = r.URL.RequestURI()
		v.Languages = s.languageOptions(p.Lang(), id.Language)
	}
	if _, ok := s.authn.(auth.TenantWriter); ok && !id.IsZero() {
		if tenants, err := s.accessibleTenants(r.Context(), id); err == nil && len(tenants) > 1 {
			v.CurrentPath = r.URL.RequestURI()
			v.TenantSwitchURL = s.url(r, "/tenant")
			for _, t := range tenants {
				v.AccessibleTenants = append(v.AccessibleTenants, TenantOption{
					ID: t.ID, Name: t.Name, Selected: id.ActiveTenantID != nil && *id.ActiveTenantID == t.ID,
				})
			}
		}
	}
	return v
}

// languageOptions builds the selector, with an "automatic" entry first so a
// preference can be cleared as well as set.
func (s *Server) languageOptions(active, stored string) []LanguageOption {
	opts := []LanguageOption{{
		Tag:      domain.LanguageAuto,
		Label:    s.bundle.T(active, "lang.auto"),
		Selected: !s.bundle.Has(stored),
	}}
	for _, tag := range s.bundle.Languages() {
		opts = append(opts, LanguageOption{
			Tag:      tag,
			Label:    s.bundle.T(active, "lang."+tag),
			Selected: tag == stored,
		})
	}
	return opts
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
func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, status int, data any) {
	layout := "base.html"
	if isFragment(r) {
		layout = "fragment.html"
	}

	tmpl, ok := s.pages[page]
	if !ok {
		s.log.Error("unknown page template", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, layout, data); err != nil {
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
	s.render(w, r, "error.html", status, struct{ view }{v})
}

// url builds a link, honouring the base path and mounting style — the
// server's configured default, or this request's mount override (see
// BasePathHeader/LinkParamHeader) when it carries one.
func (s *Server) url(r *http.Request, format string, args ...any) string {
	base, param := s.mountFor(r)
	return buildURL(base, param, fmt.Sprintf(format, args...))
}

// BasePathHeader and LinkParamHeader let a single running sidecar mount
// links differently per request.
//
// basePath/linkParam are otherwise fixed once at process startup (see
// Config.BasePath/LinkParam), which is enough when there is exactly one
// front door — but the WordPress plugin's frontend shortcode proxies through
// a different URL (admin-ajax.php) than the wp-admin proxy does
// (admin.php), and the same running binary serves both. A fragment rendered
// for one must not bake in links that point at the other.
const (
	BasePathHeader  = "X-Wpcalc-Base-Path"
	LinkParamHeader = "X-Wpcalc-Link-Param"
)

// mountFor resolves this request's base path and link param, preferring a
// per-request override over the server's configured default.
//
// Both headers are trusted unconditionally, with no signature: they only
// affect how *this server's own* links are spelled out, never who the
// caller is or what they can do — spoofing them just breaks navigation, not
// authorization. Only present the caller's own PHP shim would ever have a
// reason to set them; nothing sensitive rides on them.
func (s *Server) mountFor(r *http.Request) (base, linkParam string) {
	base, linkParam = s.basePath, s.linkParam
	if v := r.Header.Get(BasePathHeader); v != "" {
		base = v
	}
	if v, ok := headerLookup(r, LinkParamHeader); ok {
		linkParam = v
	}
	return base, linkParam
}

// headerLookup distinguishes "header present but empty" (explicitly
// path-prefix mounting, linkParam "") from "header absent" (keep the
// server's default), which r.Header.Get alone cannot: it returns "" for
// both.
func headerLookup(r *http.Request, name string) (string, bool) {
	vs, ok := r.Header[http.CanonicalHeaderKey(name)]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

// FragmentHeader asks for content without the surrounding HTML document.
//
// The WordPress plugin renders the app inside the admin page's own chrome. A
// full <html> document nested in that page would be invalid markup and would
// fight WordPress for the <head>, so the shim asks for the content alone.
const FragmentHeader = "X-Wpcalc-Fragment"

func isFragment(r *http.Request) bool { return r.Header.Get(FragmentHeader) == "1" }

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

// buildURL generates a link to an application path.
//
// Two mounting styles, because the two run modes address pages differently.
// Standalone (and behind a reverse proxy) uses a path prefix. WordPress
// addresses admin screens by query string — /wp-admin/admin.php?page=wpcalc —
// and cannot route /m/2026-07 at all, so there the application path travels as
// a query parameter and the shim hands it back to the server as a real path.
//
// Only link generation differs. The handler tree still sees ordinary paths,
// which is what keeps the WordPress mode from being a second implementation.
func buildURL(base, param, path string) string {
	if param == "" {
		return joinBase(base, path)
	}
	if path == "" {
		path = "/"
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + url.QueryEscape(param) + "=" + url.QueryEscape(path)
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

// parseAnyForm parses a request body in either form encoding.
//
// http.Request.ParseForm does not read a multipart body: it leaves PostForm
// non-nil and empty, which then stops PostFormValue from parsing it either,
// so every field reads as "" and the handler rejects a perfectly good request
// as malformed. A browser sending FormData produces exactly that, and no
// urlencoded test can see it.
func parseAnyForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(8 << 20)
	}
	return r.ParseForm()
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
	"forbidden":        "error.forbidden",
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
