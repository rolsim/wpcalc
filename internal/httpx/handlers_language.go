package httpx

import (
	"net/http"

	"source.simonet.internal/rolsim/wpcalc/internal/auth"
	"source.simonet.internal/rolsim/wpcalc/internal/domain"
)

// handleSetLanguage stores the caller's interface language.
//
// Only offered when the authenticator can persist one. Under WordPress it
// cannot: WordPress owns the user record there, and a second preference stored
// here would be a divergent answer to the same question.
func (s *Server) handleSetLanguage(w http.ResponseWriter, r *http.Request) {
	writer, ok := s.authn.(auth.LanguageWriter)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := parseAnyForm(r); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}

	lang := normaliseLangTag(r.PostFormValue("lang"))

	// Only a loaded catalog, or the empty "follow the browser" value. Storing
	// an arbitrary string would leave an account rendering in a locale that
	// does not exist, recoverable only from the database.
	if lang != domain.LanguageAuto && !s.bundle.Has(lang) {
		s.renderError(w, r, http.StatusUnprocessableEntity, "error.invalid_input")
		return
	}

	if err := writer.SetLanguage(r, lang); err != nil {
		s.log.Error("set language", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	// Back where they were, so changing the language does not also lose the
	// month someone was looking at.
	target := r.PostFormValue("return_to")
	if !isLocalPath(target) {
		target = s.url("/")
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// normaliseLangTag accepts the POSIX spelling as well as the BCP 47 one, since
// "de_CH" is what a shell locale looks like and what people type.
func normaliseLangTag(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '_' {
			r = '-'
		}
		out = append(out, r)
	}
	return string(out)
}

// isLocalPath guards the post-change redirect. Without it the form would be an
// open redirect: anything could point return_to at another host and use this
// site's domain to launder the link.
func isLocalPath(p string) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	// "//evil.example" is scheme-relative and leaves the site.
	return len(p) < 2 || p[1] != '/'
}
