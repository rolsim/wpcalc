package apiv1

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

// RequestValidator rejects any /api/v1 request that does not conform to
// openapi.yaml — a missing required field, a string that doesn't match its
// declared pattern, a value outside an enum, a body of the wrong shape —
// before it ever reaches a handler. This is real enforcement of the
// contract the spec documents, not just documentation of it: without this,
// oapi-codegen's generated code only decodes JSON structurally and
// type-coerces path/query params, and nothing checks `required`,
// `pattern`, `enum`, `minLength`, or any other constraint in the schema.
//
// Security requirements in the spec are deliberately not re-checked here
// (a no-op AuthenticationFunc): the real bearer-token check already ran in
// httpx.requireBearerAuth, earlier in the middleware chain. Validating it
// twice would just be two sources of truth for the same rule — and this
// validator has no way to resolve a token to an Identity anyway, since
// that resolution lives in internal/auth.
func RequestValidator() func(http.Handler) http.Handler {
	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec.OpenAPI3(), &nethttpmiddleware.Options{
		DoNotValidateServers: true,
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		ErrorHandlerWithOpts: requestValidationErrorHandler,
	})
}

// responseRouter matches a request back to the route it resolved to,
// purely so ResponseValidator can look up which schema a response must
// satisfy — a separate concern from RequestValidator's own internal
// routing, so a separate router rather than threading state between them.
var responseRouter = mustNewResponseRouter()

func mustNewResponseRouter() routers.Router {
	doc := spec.OpenAPI3()
	// A request reaching this router already has "/api/v1" stripped by
	// httpx (net/http.StripPrefix) before it ever reaches apiv1 — matching
	// paths against the declared `servers: [{url: /api/v1}]` entry would
	// only get in the way. RequestValidator clears this too
	// (DoNotValidateServers); doing it here as well, rather than relying on
	// that call running first, keeps this router correct regardless of
	// which runs first.
	doc.Servers = nil
	r, err := legacyrouter.NewRouter(doc)
	if err != nil {
		panic("apiv1: build response router: " + err.Error())
	}
	return r
}

// ResponseValidator verifies every JSON response actually matches
// openapi.yaml before a client sees it — buffered, so a mismatch never
// partially reaches anyone: either the real response goes out, or a clean
// 500 does. PDF responses are forwarded unvalidated and unbuffered
// (validating a binary body against `contentEncoding: binary` adds nothing,
// and buffering a whole report defeats streaming it).
//
// This exists to catch drift the other direction from RequestValidator: a
// handler that stops matching its own documented contract — a renamed
// field, a status code the spec doesn't list — is a bug in this codebase,
// not in a caller's request, so it is worth failing loudly on rather than
// silently shipping.
func (a *API) ResponseValidator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, pathParams, err := responseRouter.FindRoute(r)
		if err != nil {
			// Not a route the spec declares — RequestValidator already
			// rejected this if it matters; just pass it through.
			next.ServeHTTP(w, r)
			return
		}

		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)

		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			copyRecorded(w, rec)
			return
		}

		input := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
			},
			Status: rec.Code,
			Header: rec.Header(),
		}
		input.SetBodyBytes(rec.Body.Bytes())

		if err := openapi3filter.ValidateResponse(r.Context(), input); err != nil {
			a.log.Error("api response does not match openapi.yaml",
				"method", r.Method, "path", r.URL.Path, "status", rec.Code, "error", err)
			writeJSONError(w, http.StatusInternalServerError, "server_error", "response violated its own schema")
			return
		}
		copyRecorded(w, rec)
	})
}

func copyRecorded(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	for k, vs := range rec.Header() {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}
