package apiv1

import (
	"context"
	"strings"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

func toAPIUser(u domain.User) User {
	return User{Id: u.ID, Username: u.Username, Language: u.Language}
}

func (a *API) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageUsers) {
		return ListUsersdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	users, err := a.db.Users(ctx)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListUsersdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]User, 0, len(users))
	for _, u := range users {
		out = append(out, toAPIUser(u))
	}
	return ListUsers200JSONResponse(out), nil
}

func (a *API) CreateUser(ctx context.Context, request CreateUserRequestObject) (CreateUserResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageUsers) {
		return CreateUserdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return CreateUserdefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	userID, err := a.db.CreateUser(ctx, request.Body.Username, request.Body.Password)
	if err != nil {
		status, code := mapStoreErr(err)
		return CreateUserdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return CreateUser201JSONResponse{Id: userID, Username: request.Body.Username}, nil
}

// canActOnAccount is the "self or manage_users" rule every /users/{username}
// operation below shares: an account may always act on itself (matching the
// HTML app's self-service language switch), and manage_users system-wide
// stands in for `wpcalc user passwd`/`user lang`/`user roles`'s unscoped,
// operator-level access. Decided purely by name, with no database lookup,
// so a non-admin caller naming an account that doesn't exist gets the same
// 403 as one that does — this must never be a way to enumerate usernames.
func (a *API) canActOnAccount(ctx context.Context, username string) bool {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return false
	}
	return strings.EqualFold(id.Username, username) || id.CanSystemWide(domain.PermManageUsers)
}

func (a *API) GetUserRoles(ctx context.Context, request GetUserRolesRequestObject) (GetUserRolesResponseObject, error) {
	if !a.canActOnAccount(ctx, request.Username) {
		return GetUserRolesdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	target, err := a.db.UserByUsername(ctx, request.Username)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetUserRolesdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	roles, err := a.db.UserRolesForUser(ctx, target.ID)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetUserRolesdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]UserRoleAssignment, 0, len(roles))
	for _, r := range roles {
		out = append(out, UserRoleAssignment{RoleId: r.RoleID, TenantId: r.TenantID, EmployeeId: r.EmployeeID})
	}
	return GetUserRoles200JSONResponse(out), nil
}

func (a *API) SetUserPassword(ctx context.Context, request SetUserPasswordRequestObject) (SetUserPasswordResponseObject, error) {
	if !a.canActOnAccount(ctx, request.Username) {
		return SetUserPassworddefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return SetUserPassworddefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if err := a.db.SetPassword(ctx, request.Username, request.Body.Password); err != nil {
		status, code := mapStoreErr(err)
		return SetUserPassworddefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return SetUserPassword204Response{}, nil
}

func (a *API) SetUserLanguage(ctx context.Context, request SetUserLanguageRequestObject) (SetUserLanguageResponseObject, error) {
	if !a.canActOnAccount(ctx, request.Username) {
		return SetUserLanguagedefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	target, err := a.db.UserByUsername(ctx, request.Username)
	if err != nil {
		status, code := mapStoreErr(err)
		return SetUserLanguagedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	lang := ""
	if request.Body != nil && request.Body.Lang != nil {
		lang = *request.Body.Lang
	}
	if err := a.db.SetUserLanguage(ctx, target.ID, lang); err != nil {
		status, code := mapStoreErr(err)
		return SetUserLanguagedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return SetUserLanguage204Response{}, nil
}
