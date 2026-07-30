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

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/i18n"
	"github.com/rolsim/wpcalc/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server holds the dependencies shared by every handler.
type Server struct {
	db       *store.DB
	bundle   *i18n.Bundle
	authn    auth.Authenticator
	log      *slog.Logger
	pages    map[string]*template.Template
	connKind auth.ConnKind

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

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("httpx: static assets missing: " + err.Error())
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

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
