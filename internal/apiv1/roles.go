package apiv1

import (
	"context"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

func (a *API) ListPermissions(ctx context.Context, _ ListPermissionsRequestObject) (ListPermissionsResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return ListPermissionsdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	perms, err := a.db.Permissions(ctx)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListPermissionsdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]Permission, 0, len(perms))
	for _, p := range perms {
		out = append(out, toAPIPermission(p))
	}
	return ListPermissions200JSONResponse(out), nil
}

func (a *API) ListRoles(ctx context.Context, _ ListRolesRequestObject) (ListRolesResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return ListRolesdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	roles, err := a.db.Roles(ctx)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListRolesdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	roleIDs := make([]string, 0, len(roles))
	for _, r := range roles {
		roleIDs = append(roleIDs, r.ID)
	}
	perms, err := a.db.RolePermissionsFor(ctx, roleIDs)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListRolesdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		out = append(out, toAPIRole(r, perms[r.ID]))
	}
	return ListRoles200JSONResponse(out), nil
}

func (a *API) CreateRole(ctx context.Context, request CreateRoleRequestObject) (CreateRoleResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return CreateRoledefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return CreateRoledefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	scope := domain.Scope(request.Body.Scope)
	if err := a.db.CreateRole(ctx, request.Body.Id, request.Body.Name, scope); err != nil {
		status, code := mapStoreErr(err)
		return CreateRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return CreateRole201JSONResponse(toAPIRole(domain.Role{
		ID: request.Body.Id, Name: request.Body.Name, Scope: scope,
	}, nil)), nil
}

func (a *API) DeleteRole(ctx context.Context, request DeleteRoleRequestObject) (DeleteRoleResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return DeleteRoledefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if err := a.db.DeleteRole(ctx, request.RoleId); err != nil {
		status, code := mapStoreErr(err)
		return DeleteRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return DeleteRole204Response{}, nil
}

func (a *API) AddRolePermission(ctx context.Context, request AddRolePermissionRequestObject) (AddRolePermissionResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return AddRolePermissiondefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return AddRolePermissiondefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if err := a.db.AddRolePermission(ctx, request.RoleId, string(request.Body.PermissionId)); err != nil {
		status, code := mapStoreErr(err)
		return AddRolePermissiondefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return AddRolePermission204Response{}, nil
}

func (a *API) RemoveRolePermission(ctx context.Context, request RemoveRolePermissionRequestObject) (RemoveRolePermissionResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return RemoveRolePermissiondefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if err := a.db.RemoveRolePermission(ctx, request.RoleId, string(request.PermissionId)); err != nil {
		status, code := mapStoreErr(err)
		return RemoveRolePermissiondefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return RemoveRolePermission204Response{}, nil
}
