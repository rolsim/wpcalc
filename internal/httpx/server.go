// Package httpx is the web layer: one handler tree, served identically in
// standalone and WordPress-sidecar modes.
//
// Nothing in here knows which mode it is running under. The only differences
// live in the Authenticator that is injected and the listener the caller
// binds, which is what makes the WordPress plugin an addition rather than a
// second implementation.
package httpx

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rolsim/wpcalc/internal/apiv1"
	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/i18n"
	"github.com/rolsim/wpcalc/internal/specdoc"
	"github.com/rolsim/wpcalc/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed openapi.yaml
var appSpecYAML []byte

// appSpec documents the HTML app itself (this package) — a separate,
// larger document from internal/apiv1's, since almost none of these routes
// return JSON or take a JSON body the way /api/v1's do.
var appSpec = specdoc.MustParse(appSpecYAML)

// Server holds the dependencies shared by every handler.
type Server struct {
	db       *store.DB
	bundle   *i18n.Bundle
	authn    auth.Authenticator
	log      *slog.Logger
	pages    map[string]*template.Template
	connKind auth.ConnKind

	// api and apiAuthn back /api/v1 — a separate, stateless, bearer-token
	// authenticated JSON front end onto the same store, mounted alongside
	// the HTML app rather than replacing any of it.
	api      *apiv1.API
	apiAuthn auth.Authenticator

	// linkParam, when set, makes generated links carry the application path
	// as a query parameter instead of as a path prefix. WordPress addresses
	// admin screens by query string and cannot route /m/2026-07 at all.
	linkParam string

	// basePath prefixes generated URLs. WordPress serves the app under an
	// admin page rather than at the site root, so links must be built rather
	// than hardcoded to "/".
	basePath string
}

// Config configures a Server.
type Config struct {
	DB       *store.DB
	Bundle   *i18n.Bundle
	Auth     auth.Authenticator
	Logger   *slog.Logger
	ConnKind auth.ConnKind
	BasePath string

	// LinkParam switches link generation to query-parameter mounting. Empty
	// means path-prefix mounting, which is what standalone uses.
	LinkParam string
}

// New builds the server and parses templates once at startup, so a broken
// template fails the process rather than the first request that reaches it.
func New(cfg Config) (*Server, error) {
	if cfg.DB == nil {
		return nil, errors.New("httpx: DB is required")
	}
	if cfg.Bundle == nil {
		return nil, errors.New("httpx: i18n bundle is required")
	}
	if cfg.Auth == nil {
		return nil, errors.New("httpx: authenticator is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ConnKind == "" {
		cfg.ConnKind = auth.ConnTCP
	}

	s := &Server{
		db:        cfg.DB,
		bundle:    cfg.Bundle,
		authn:     cfg.Auth,
		log:       cfg.Logger,
		connKind:  cfg.ConnKind,
		linkParam: cfg.LinkParam,
		api:       apiv1.New(cfg.DB, cfg.Bundle, cfg.Logger),
		apiAuthn:  auth.NewBearerTokens(cfg.DB),
	}

	// A query-parameter base is a full URL with its own query string, so it
	// must not be run through the path normaliser.
	if cfg.LinkParam == "" {
		s.basePath = normaliseBase(cfg.BasePath)
	} else {
		s.basePath = strings.TrimSpace(cfg.BasePath)
	}

	// Parse at startup so a broken template kills the process rather than the
	// first request unlucky enough to reach it.
	s.pages = make(map[string]*template.Template, len(pageTemplates))
	for _, page := range pageTemplates {
		tmpl, err := template.New(page).
			Funcs(s.funcMap()).
			ParseFS(templatesFS, "templates/base.html", "templates/fragment.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("httpx: parse %s: %w", page, err)
		}
		s.pages[page] = tmpl
	}
	return s, nil
}

// Handler returns the fully wrapped handler tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated: the health check the PHP shim polls to decide whether
	// the sidecar needs restarting, and the login form itself.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// The app's own OpenAPI document — documentation, so it needs no
	// identity any more than /healthz does. /openapi.html is interactive
	// (Swagger UI, vendored — see internal/specdoc); its JS/CSS live under
	// /openapi-assets/, the sibling path ServeHTML's page expects.
	mux.HandleFunc("GET /openapi.json", appSpec.ServeJSON)
	mux.HandleFunc("GET /openapi.yaml", appSpec.ServeYAML)
	mux.HandleFunc("GET /openapi.html", appSpec.ServeHTML)
	mux.Handle("GET /openapi-assets/", http.StripPrefix("/openapi-assets/", specdoc.AssetsHandler()))

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("httpx: static assets missing: " + err.Error())
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	// /api/v1: a stateless JSON mirror of the app, authenticated with a
	// bearer token instead of the session cookie. Mounted as its own
	// sub-tree — requireBearerAuth, not requireAuth, wraps it — so a
	// bearer token never grants access to the HTML app and a session
	// cookie is never accepted here.
	//
	// The generated layer alone only decodes JSON structurally and
	// type-coerces path/query params — nothing in it enforces `required`,
	// `pattern`, `enum`, or any other constraint openapi.yaml declares, and
	// its default error responses are plain text, not JSON. Both gaps are
	// closed here: apiv1.RequestValidator rejects a request that does not
	// conform to the spec before any handler runs, s.api.ResponseValidator
	// verifies a response does either, and every generated-layer error hook
	// is pointed at apiv1.RequestErrorHandler so a failure at any layer
	// comes back as the same {"error": "..."} shape a handler's own errors
	// already used.
	strict := apiv1.NewStrictHandlerWithOptions(s.api, nil, apiv1.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  apiv1.RequestErrorHandler,
		ResponseErrorHandlerFunc: apiv1.RequestErrorHandler,
	})
	generated := apiv1.HandlerWithOptions(strict, apiv1.StdHTTPServerOptions{
		ErrorHandlerFunc: apiv1.RequestErrorHandler,
	})
	apiMux := http.NewServeMux()
	apiMux.Handle("/", s.api.ResponseValidator(apiv1.RequestValidator()(generated)))
	apiMux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) { apiv1.ServeSpecJSON(w, r) })
	apiMux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) { apiv1.ServeSpecYAML(w, r) })
	apiMux.HandleFunc("GET /openapi.html", func(w http.ResponseWriter, r *http.Request) { apiv1.ServeSpecHTML(w, r) })
	apiMux.Handle("GET /openapi-assets/", http.StripPrefix("/openapi-assets/", specdoc.AssetsHandler()))
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", s.requireBearerAuth(apiMux)))

	// Everything else requires an identity.
	protected := http.NewServeMux()
	s.routes(protected)
	mux.Handle("/", s.requireAuth(protected))

	// Outermost first: a panic in any handler, including the auth middleware,
	// must still produce a response rather than a dropped connection.
	return s.recoverPanic(s.tagConn(s.logRequests(mux)))
}

// routes registers the authenticated handler tree. Kept separate so tests can
// mount it without the auth wrapper.
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /m/{ym}", s.handleGrid)
	mux.HandleFunc("POST /m/{ym}/hours", s.handleSetHours)
	mux.HandleFunc("POST /m/{ym}/comment", s.handleSetComment)

	mux.HandleFunc("GET /employees", s.handleEmployeeList)
	mux.HandleFunc("GET /employees/new", s.handleEmployeeNew)
	mux.HandleFunc("POST /employees", s.handleEmployeeCreate)
	mux.HandleFunc("GET /employees/{id}/edit", s.handleEmployeeEdit)
	mux.HandleFunc("POST /employees/{id}", s.handleEmployeeUpdate)
	mux.HandleFunc("POST /employees/{id}/delete", s.handleEmployeeDelete)

	mux.HandleFunc("POST /language", s.handleSetLanguage)

	mux.HandleFunc("GET /reports", s.handleReportIndex)
	mux.HandleFunc("GET /report/month/{ym}", s.handleReportMonth)
	mux.HandleFunc("GET /report/employee/{id}/month/{ym}", s.handleReportEmployeeMonth)
	mux.HandleFunc("GET /report/employee/{id}/year/{year}", s.handleReportEmployeeYear)

	// Tenant switcher: available to any signed-in account, since which
	// tenants it lists is already scoped to what that account can reach.
	mux.HandleFunc("GET /tenants/choose", s.handleTenantChoose)
	mux.HandleFunc("POST /tenant", s.handleTenantSwitch)

	// Tenant list/create: system-wide, manage_tenants only.
	mux.Handle("GET /tenants", s.requireSystemPermission(domain.PermManageTenants, http.HandlerFunc(s.handleTenantList)))
	mux.Handle("POST /tenants", s.requireSystemPermission(domain.PermManageTenants, http.HandlerFunc(s.handleTenantCreate)))

	// Per-tenant access matrix (employee-scope roles only): each handler
	// checks manage_users against the {id} in the path itself, since that
	// permission is tenant-specific rather than system-wide.
	mux.HandleFunc("GET /tenants/{id}/access", s.handleTenantAccess)
	mux.HandleFunc("POST /tenants/{id}/access", s.handleTenantAccessGrant)
	mux.HandleFunc("POST /tenants/{id}/access/revoke", s.handleTenantAccessRevoke)

	// Role management: system-wide, manage_roles only. The one place that
	// can create another super-admin or mandant-admin.
	mux.Handle("GET /roles", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRoleList)))
	mux.Handle("POST /roles", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRoleCreate)))
	mux.Handle("POST /roles/{id}/delete", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRoleDelete)))
	mux.Handle("POST /roles/{id}/permissions", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRolePermissionAdd)))
	mux.Handle("POST /roles/{id}/permissions/remove", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRolePermissionRemove)))
	mux.Handle("POST /roles/assign", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRoleAssign)))
	mux.Handle("POST /roles/revoke", s.requireSystemPermission(domain.PermManageRoles, http.HandlerFunc(s.handleRoleRevoke)))
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.url("/m/%s", currentMonth()), http.StatusSeeOther)
}
