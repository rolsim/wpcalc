package httpx

import (
	"net/http"

	"source.simonet.internal/rolsim/wpcalc/internal/auth"
)

type loginView struct {
	view
	Failed bool
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	// Under WordPress the identity comes from WordPress; offering a second,
	// weaker way into the same data would undo the point of the shim.
	if _, ok := s.authn.(auth.SessionWriter); !ok {
		http.NotFound(w, r)
		return
	}
	if id, err := s.authn.Identify(r); err == nil && !id.IsZero() {
		http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", http.StatusOK, loginView{view: s.newView(r, "auth.login")})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	sw, ok := s.authn.(auth.SessionWriter)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}

	err := sw.Login(w, r.PostFormValue("username"), r.PostFormValue("password"))
	if err != nil {
		// Deliberately vague and status 401 either way: distinguishing "no
		// such user" from "wrong password" would enumerate accounts for free
		// once P3 replaces this with real ones.
		s.log.Warn("failed login", "remote", r.RemoteAddr)
		v := s.newView(r, "auth.login")
		v.Error = v.T("auth.failed")
		s.render(w, "login.html", http.StatusUnauthorized, loginView{view: v, Failed: true})
		return
	}
	http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sw, ok := s.authn.(auth.SessionWriter); ok {
		sw.Logout(w)
	}
	http.Redirect(w, r, s.url("/login"), http.StatusSeeOther)
}
