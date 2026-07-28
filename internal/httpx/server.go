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

	"source.simonet.internal/rolsim/wpcalc/internal/auth"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
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
		db:       cfg.DB,
		bundle:   cfg.Bundle,
		authn:    cfg.Auth,
		log:      cfg.Logger,
		connKind: cfg.ConnKind,
		basePath: normaliseBase(cfg.BasePath),
	}

	// Parse at startup so a broken template kills the process rather than the
	// first request unlucky enough to reach it.
	s.pages = make(map[string]*template.Template, len(pageTemplates))
	for _, page := range pageTemplates {
		tmpl, err := template.New(page).
			Funcs(s.funcMap()).
			ParseFS(templatesFS, "templates/base.html", "templates/"+page)
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
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.url("/m/%s", currentMonth()), http.StatusSeeOther)
}
