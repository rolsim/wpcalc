package httpx

import (
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rolsim/wpcalc/internal/auth"
)

// tagConn records which listener accepted the request.
//
// The WordPress authenticator trusts identity headers only over the unix
// socket, so this tag is a security control and not diagnostics. It is set
// from how the server was started, never from anything the client sends.
func (s *Server) tagConn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithConnKind(r.Context(), s.connKind)))
	})
}

// recoverPanic turns a handler panic into a 500 rather than a dropped
// connection. Under the WordPress shim a dropped connection surfaces as an
// unexplained blank admin page, which is far harder to diagnose than a 500.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic in handler",
					"panic", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Millisecond).String())
	})
}

// requireAuth resolves the identity or turns the request away.
//
// Browsers get a redirect to the login form; anything that looks like a
// programmatic call gets a 401, because redirecting an XHR to an HTML login
// page produces a confusing 200 full of markup instead of a clear failure.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.authn.Identify(r)
		if err == nil && !id.IsZero() {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
			return
		}

		// Misconfiguration, not merely a missing session: say so in the log,
		// because the request looked like a forgery attempt or a shim wired to
		// the wrong listener.
		if errors.Is(err, auth.ErrHeadersOverTCP) {
			s.log.Error("identity headers presented over a TCP listener; refusing",
				"path", r.URL.Path, "remote", r.RemoteAddr)
		}

		if wantsJSON(r) {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, s.url("/login"), http.StatusSeeOther)
	})
}

// requireSystemPermission turns away a signed-in identity that does not hold
// this permission at system scope — used for the platform-wide admin pages
// (/tenants, /roles) that are not scoped to any one tenant.
//
// A 403 rather than a redirect: the caller is authenticated, just not
// permitted, and sending them back to a login form they already passed would
// misdescribe the failure.
func (s *Server) requireSystemPermission(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.IdentityFrom(r.Context())
		if !ok || !id.CanSystemWide(permission) {
			s.renderError(w, r, http.StatusForbidden, "error.forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireBearerAuth is requireAuth's counterpart for /api/v1: every failure
// is a JSON 401, never a redirect — a script calling a JSON API has nowhere
// useful to follow an HTML login page to. `openapi.json`/`.yaml`/`.html`,
// its Swagger UI assets, and `healthz` are exempted (checked against the
// path with the /api/v1 prefix already stripped, since that is what this
// middleware wraps), matching the root app's own `/healthz` being
// reachable without a session — Swagger UI's own page load (fetching the
// spec and its JS/CSS) must work before anyone has a token to authorize
// with.
func (s *Server) requireBearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/openapi.json", "/openapi.yaml", "/openapi.html":
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/openapi-assets/") {
			next.ServeHTTP(w, r)
			return
		}

		id, err := s.apiAuthn.Identify(r)
		if err != nil || id.IsZero() {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
	})
}

// wantsJSON reports whether the caller is a script rather than a navigating
// browser, so failures can be reported in a form it can act on.
func wantsJSON(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}
