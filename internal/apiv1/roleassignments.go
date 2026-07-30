package apiv1

import (
	"context"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
)

func (a *API) ListRoleAssignments(ctx context.Context, _ ListRoleAssignmentsRequestObject) (ListRoleAssignmentsResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return ListRoleAssignmentsdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	assignments, err := a.db.AdminRoleAssignments(ctx)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListRoleAssignmentsdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]AdminRoleAssignment, 0, len(assignments))
	for _, ra := range assignments {
		out = append(out, toAPIAdminAssignment(ra))
	}
	return ListRoleAssignments200JSONResponse(out), nil
}

func (a *API) GrantRole(ctx context.Context, request GrantRoleRequestObject) (GrantRoleResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return GrantRoledefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return GrantRoledefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	u, err := a.db.UserByUsername(ctx, request.Body.Username)
	if err != nil {
		status, code := mapStoreErr(err)
		return GrantRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	if err := a.db.GrantUserRole(ctx, u.ID, request.Body.TenantId, nil, request.Body.RoleId); err != nil {
		status, code := mapStoreErr(err)
		return GrantRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return GrantRole204Response{}, nil
}

func (a *API) RevokeRole(ctx context.Context, request RevokeRoleRequestObject) (RevokeRoleResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageRoles) {
		return RevokeRoledefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return RevokeRoledefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if err := a.db.RevokeUserRole(ctx, request.Body.UserId, request.Body.TenantId, nil); err != nil {
		status, code := mapStoreErr(err)
		return RevokeRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return RevokeRole204Response{}, nil
}

func (a *API) ListEmployeeRoleAssignments(ctx context.Context, request ListEmployeeRoleAssignmentsRequestObject) (ListEmployeeRoleAssignmentsResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageUsers, request.TenantId) {
		return ListEmployeeRoleAssignmentsdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	assignments, err := a.db.EmployeeRoleAssignmentsForTenant(ctx, request.TenantId)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListEmployeeRoleAssignmentsdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]EmployeeRoleAssignment, 0, len(assignments))
	for _, ra := range assignments {
		out = append(out, toAPIEmployeeAssignment(ra))
	}
	return ListEmployeeRoleAssignments200JSONResponse(out), nil
}

func (a *API) GrantEmployeeRole(ctx context.Context, request GrantEmployeeRoleRequestObject) (GrantEmployeeRoleResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageUsers, request.TenantId) {
		return GrantEmployeeRoledefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return GrantEmployeeRoledefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if err := a.checkEmployeeInTenant(ctx, request.Body.EmployeeId, request.TenantId); err != nil {
		status, code := mapStoreErr(err)
		return GrantEmployeeRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	u, err := a.db.UserByUsername(ctx, request.Body.Username)
	if err != nil {
		status, code := mapStoreErr(err)
		return GrantEmployeeRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	employeeID := request.Body.EmployeeId
	if err := a.db.GrantUserRole(ctx, u.ID, nil, &employeeID, request.Body.RoleId); err != nil {
		status, code := mapStoreErr(err)
		return GrantEmployeeRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return GrantEmployeeRole204Response{}, nil
}

func (a *API) RevokeEmployeeRole(ctx context.Context, request RevokeEmployeeRoleRequestObject) (RevokeEmployeeRoleResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageUsers, request.TenantId) {
		return RevokeEmployeeRoledefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return RevokeEmployeeRoledefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if err := a.checkEmployeeInTenant(ctx, request.Body.EmployeeId, request.TenantId); err != nil {
		status, code := mapStoreErr(err)
		return RevokeEmployeeRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	employeeID := request.Body.EmployeeId
	if err := a.db.RevokeUserRole(ctx, request.Body.UserId, nil, &employeeID); err != nil {
		status, code := mapStoreErr(err)
		return RevokeEmployeeRoledefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return RevokeEmployeeRole204Response{}, nil
}

// checkEmployeeInTenant guards against granting/revoking a role against an
// employee id from a different tenant than the one the caller was
// authorized against — the tenant isolation boundary, enforced the same
// way GetEmployee/UpdateEmployee/DeleteEmployee enforce it.
func (a *API) checkEmployeeInTenant(ctx context.Context, employeeID, tenantID int64) error {
	e, err := a.db.Employee(ctx, employeeID)
	if err != nil {
		return err
	}
	if e.TenantID != tenantID {
		return store.ErrNotFound
	}
	return nil
}
