package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
)

// mapStoreErr turns a store/domain sentinel into an HTTP status and a
// short, stable, non-localized error code — the JSON-API equivalent of the
// HTML app's ?err= redirect tokens, but as a real status code plus body
// rather than a query string a client would have to parse out of a
// Location header.
func mapStoreErr(err error) (status int, code string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrDuplicateTenant):
		return http.StatusConflict, "duplicate_tenant"
	case errors.Is(err, store.ErrDuplicateRole):
		return http.StatusConflict, "duplicate_role"
	case errors.Is(err, store.ErrDuplicateUsername):
		return http.StatusConflict, "duplicate_username"
	case errors.Is(err, store.ErrRoleAlreadyAssignedDifferently):
		return http.StatusConflict, "role_already_assigned_differently"
	case errors.Is(err, store.ErrRoleScopeTooNarrow):
		return http.StatusUnprocessableEntity, "role_scope_too_narrow"
	case errors.Is(err, domain.ErrNotEmployed):
		return http.StatusUnprocessableEntity, "not_employed"
	case errors.Is(err, domain.ErrHoursRange):
		return http.StatusUnprocessableEntity, "hours_range"
	case errors.Is(err, domain.ErrInvalidEmployee):
		return http.StatusUnprocessableEntity, "invalid_employee"
	case errors.Is(err, domain.ErrInvalidTenant):
		return http.StatusUnprocessableEntity, "invalid_tenant"
	case errors.Is(err, domain.ErrInvalidRole):
		return http.StatusUnprocessableEntity, "invalid_role"
	case errors.Is(err, domain.ErrInvalidScope):
		return http.StatusUnprocessableEntity, "invalid_scope"
	case errors.Is(err, domain.ErrInvalidUser):
		return http.StatusUnprocessableEntity, "invalid_user"
	default:
		return http.StatusInternalServerError, "server_error"
	}
}

const (
	codeForbidden    = "forbidden"
	codeUnauthorized = "unauthenticated"
	codeBadRequest   = "invalid_input"
	codeNotFound     = "not_found"
)

// writeJSONError writes {"error": code, "message": message} — the one
// error shape every /api/v1 response uses, whether the failure came from a
// handler's own business logic or from the generated/validation plumbing
// in front of it. message is omitted when empty; it exists for
// generated-layer failures (a malformed body, an unparsable path param) a
// stable `error` code alone would describe too vaguely to debug.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := Error{Error: code}
	if message != "" {
		body.Message = &message
	}
	_ = json.NewEncoder(w).Encode(body)
}

// RequestErrorHandler matches the signature every generated-layer error
// hook shares (StrictHTTPServerOptions.RequestErrorHandlerFunc /
// ResponseErrorHandlerFunc, StdHTTPServerOptions.ErrorHandlerFunc): a
// malformed JSON body, an unparsable path/query parameter, or — via
// ResponseErrorHandlerFunc — a Go error a strict handler method returned
// directly instead of a typed response (none of ours do, but the hook must
// still produce JSON if one ever does). Exported so internal/httpx can wire
// it into every one of those hooks when it builds the generated handler.
func RequestErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
}

// requestValidationErrorHandler adapts nethttp-middleware's richer error
// callback — it knows the status the OpenAPI validator suggests, which is
// more precise than always answering 400 (an unmatched route, for
// instance, suggests 404).
func requestValidationErrorHandler(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
	status := opts.StatusCode
	if status == 0 {
		status = http.StatusBadRequest
	}
	writeJSONError(w, status, "invalid_request", err.Error())
}
